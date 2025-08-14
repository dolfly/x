package relay

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"time"

	"github.com/dolfly/core/limiter"
	"github.com/dolfly/core/logger"
	"github.com/dolfly/core/observer/stats"
	"github.com/dolfly/relay"
	xbypass "github.com/dolfly/x/bypass"
	xctx "github.com/dolfly/x/ctx"
	ictx "github.com/dolfly/x/internal/ctx"
	xnet "github.com/dolfly/x/internal/net"
	serial "github.com/dolfly/x/internal/util/serial"
	"github.com/dolfly/x/internal/util/sniffing"
	traffic_wrapper "github.com/dolfly/x/limiter/traffic/wrapper"
	stats_wrapper "github.com/dolfly/x/observer/stats/wrapper"
	xrecorder "github.com/dolfly/x/recorder"
)

func (h *relayHandler) handleConnect(ctx context.Context, conn net.Conn, network, address string, ro *xrecorder.HandlerRecorderObject, log logger.Logger) (err error) {
	if network == "unix" || network == "serial" {
		if host, _, _ := net.SplitHostPort(address); host != "" {
			address = host
		}
	}

	log = log.WithFields(map[string]any{
		"dst":  address,
		"cmd":  "connect",
		"host": address,
	})

	log.Debugf("%s >> %s/%s", conn.RemoteAddr(), address, network)

	{
		clientID := xctx.ClientIDFromContext(ctx)
		rw := traffic_wrapper.WrapReadWriter(
			h.limiter,
			conn,
			string(clientID),
			limiter.ScopeOption(limiter.ScopeClient),
			limiter.ServiceOption(h.options.Service),
			limiter.NetworkOption(network),
			limiter.AddrOption(address),
			limiter.ClientOption(string(clientID)),
			limiter.SrcOption(conn.RemoteAddr().String()),
		)
		if h.options.Observer != nil {
			pstats := h.stats.Stats(string(clientID))
			pstats.Add(stats.KindTotalConns, 1)
			pstats.Add(stats.KindCurrentConns, 1)
			defer pstats.Add(stats.KindCurrentConns, -1)
			rw = stats_wrapper.WrapReadWriter(rw, pstats)
		}

		conn = xnet.NewReadWriteConn(rw, rw, conn)
	}

	resp := relay.Response{
		Version: relay.Version1,
		Status:  relay.StatusOK,
	}

	if address == "" {
		resp.Status = relay.StatusBadRequest
		resp.WriteTo(conn)
		err = errors.New("target not specified")
		log.Error(err)
		return
	}

	if h.options.Bypass != nil && h.options.Bypass.Contains(ctx, network, address) {
		log.Debug("bypass: ", address)
		resp.Status = relay.StatusForbidden
		resp.WriteTo(conn)
		return xbypass.ErrBypass
	}

	switch h.md.hash {
	case "host":
		ctx = xctx.ContextWithHash(ctx, &xctx.Hash{Source: address})
	}

	var cc net.Conn
	switch network {
	case "unix":
		cc, err = (&net.Dialer{}).DialContext(ctx, "unix", address)
	case "serial":
		var port io.ReadWriteCloser
		port, err = serial.OpenPort(serial.ParseConfigFromAddr(address))
		if err == nil {
			cc = &serialConn{
				ReadWriteCloser: port,
				port:            address,
			}
		}
	default:
		var buf bytes.Buffer
		cc, err = h.options.Router.Dial(ictx.ContextWithBuffer(ctx, &buf), network, address)
		ro.Route = buf.String()
	}
	if err != nil {
		resp.Status = relay.StatusNetworkUnreachable
		resp.WriteTo(conn)
		return err
	}
	defer cc.Close()

	log = log.WithFields(map[string]any{"src": cc.LocalAddr().String(), "dst": cc.RemoteAddr().String()})
	ro.SrcAddr = cc.LocalAddr().String()
	ro.DstAddr = cc.RemoteAddr().String()

	if h.md.noDelay {
		if _, err := resp.WriteTo(conn); err != nil {
			log.Error(err)
			return err
		}
	}

	switch network {
	case "udp", "udp4", "udp6":
		rc := &udpConn{
			Conn: conn,
		}
		if !h.md.noDelay {
			// cache the header
			if _, err := resp.WriteTo(&rc.wbuf); err != nil {
				return err
			}
		}
		conn = rc
	default:
		if !h.md.noDelay {
			rc := &tcpConn{
				Conn: conn,
			}
			// cache the header
			if _, err := resp.WriteTo(&rc.wbuf); err != nil {
				return err
			}
			conn = rc
		}
	}

	if h.md.sniffing {
		if h.md.sniffingTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(h.md.sniffingTimeout))
		}

		br := bufio.NewReader(conn)
		proto, _ := sniffing.Sniff(ctx, br)
		ro.Proto = proto

		if h.md.sniffingTimeout > 0 {
			conn.SetReadDeadline(time.Time{})
		}

		dial := func(ctx context.Context, network, address string) (net.Conn, error) {
			return cc, nil
		}
		dialTLS := func(ctx context.Context, network, address string, cfg *tls.Config) (net.Conn, error) {
			return cc, nil
		}
		sniffer := &sniffing.Sniffer{
			Websocket:           h.md.sniffingWebsocket,
			WebsocketSampleRate: h.md.sniffingWebsocketSampleRate,
			Recorder:            h.recorder.Recorder,
			RecorderOptions:     h.recorder.Options,
			Certificate:         h.md.certificate,
			PrivateKey:          h.md.privateKey,
			NegotiatedProtocol:  h.md.alpn,
			CertPool:            h.certPool,
			MitmBypass:          h.md.mitmBypass,
			ReadTimeout:         h.md.readTimeout,
		}

		conn = xnet.NewReadWriteConn(br, conn, conn)
		switch proto {
		case sniffing.ProtoHTTP:
			return sniffer.HandleHTTP(ctx, "tcp", conn,
				sniffing.WithDial(dial),
				sniffing.WithDialTLS(dialTLS),
				sniffing.WithRecorderObject(ro),
				sniffing.WithLog(log),
			)
		case sniffing.ProtoTLS:
			return sniffer.HandleTLS(ctx, "tcp", conn,
				sniffing.WithDial(dial),
				sniffing.WithDialTLS(dialTLS),
				sniffing.WithRecorderObject(ro),
				sniffing.WithLog(log),
			)
		}
	}

	t := time.Now()
	log.Infof("%s <-> %s", conn.RemoteAddr(), address)
	// xnet.Transport(conn, cc)
	xnet.Pipe(ctx, conn, cc)
	log.WithFields(map[string]any{
		"duration": time.Since(t),
	}).Infof("%s >-< %s", conn.RemoteAddr(), address)

	return nil
}

type serialConn struct {
	io.ReadWriteCloser
	port string
}

func (c *serialConn) LocalAddr() net.Addr {
	return &serialAddr{
		port: "@",
	}
}

func (c *serialConn) RemoteAddr() net.Addr {
	return &serialAddr{
		port: c.port,
	}
}

func (c *serialConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *serialConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *serialConn) SetWriteDeadline(t time.Time) error {
	return nil
}

type serialAddr struct {
	port string
}

func (a *serialAddr) Network() string {
	return "serial"
}

func (a *serialAddr) String() string {
	return a.port
}
