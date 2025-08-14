package unix

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"time"

	"github.com/dolfly/core/chain"
	"github.com/dolfly/core/handler"
	"github.com/dolfly/core/hop"
	"github.com/dolfly/core/logger"
	md "github.com/dolfly/core/metadata"
	"github.com/dolfly/core/observer/stats"
	"github.com/dolfly/core/recorder"
	ctxvalue "github.com/dolfly/x/ctx"
	xio "github.com/dolfly/x/internal/io"
	xnet "github.com/dolfly/x/internal/net"
	"github.com/dolfly/x/internal/util/sniffing"
	tls_util "github.com/dolfly/x/internal/util/tls"
	xstats "github.com/dolfly/x/observer/stats"
	stats_wrapper "github.com/dolfly/x/observer/stats/wrapper"
	xrecorder "github.com/dolfly/x/recorder"
	"github.com/dolfly/x/registry"
)

func init() {
	registry.HandlerRegistry().Register("unix", NewHandler)
}

type unixHandler struct {
	hop      hop.Hop
	md       metadata
	options  handler.Options
	recorder recorder.RecorderObject
	certPool tls_util.CertPool
}

func NewHandler(opts ...handler.Option) handler.Handler {
	options := handler.Options{}
	for _, opt := range opts {
		opt(&options)
	}

	return &unixHandler{
		options: options,
	}
}

func (h *unixHandler) Init(md md.Metadata) (err error) {
	if err = h.parseMetadata(md); err != nil {
		return
	}

	for _, ro := range h.options.Recorders {
		if ro.Record == xrecorder.RecorderServiceHandler {
			h.recorder = ro
			break
		}
	}

	if h.md.certificate != nil && h.md.privateKey != nil {
		h.certPool = tls_util.NewMemoryCertPool()
	}

	return
}

// Forward implements handler.Forwarder.
func (h *unixHandler) Forward(hop hop.Hop) {
	h.hop = hop
}

func (h *unixHandler) Handle(ctx context.Context, conn net.Conn, opts ...handler.HandleOption) (err error) {
	defer conn.Close()

	start := time.Now()

	ro := &xrecorder.HandlerRecorderObject{
		Service:    h.options.Service,
		Network:    "unix",
		RemoteAddr: conn.RemoteAddr().String(),
		LocalAddr:  conn.LocalAddr().String(),
		Time:       start,
		SID:        string(ctxvalue.SidFromContext(ctx)),
	}
	ro.ClientIP, _, _ = net.SplitHostPort(conn.RemoteAddr().String())

	log := h.options.Logger.WithFields(map[string]any{
		"remote":  conn.RemoteAddr().String(),
		"local":   conn.LocalAddr().String(),
		"sid":     ctxvalue.SidFromContext(ctx),
		"client":  ro.ClientIP,
		"network": ro.Network,
	})
	log.Infof("%s <> %s", conn.RemoteAddr(), conn.LocalAddr())

	pStats := xstats.Stats{}
	conn = stats_wrapper.WrapConn(conn, &pStats)

	defer func() {
		if err != nil {
			ro.Err = err.Error()
		}
		ro.InputBytes = pStats.Get(stats.KindInputBytes)
		ro.OutputBytes = pStats.Get(stats.KindOutputBytes)
		ro.Duration = time.Since(start)
		if err := ro.Record(ctx, h.recorder.Recorder); err != nil {
			log.Errorf("record: %v", err)
		}

		log.WithFields(map[string]any{
			"duration":    time.Since(start),
			"inputBytes":  ro.InputBytes,
			"outputBytes": ro.OutputBytes,
		}).Infof("%s >< %s", conn.RemoteAddr(), conn.LocalAddr())
	}()

	if h.hop != nil {
		target := h.hop.Select(ctx)
		if target == nil {
			err = errors.New("target not available")
			log.Error(err)
			return err
		}
		log = log.WithFields(map[string]any{
			"node": target.Name,
			"dst":  target.Addr,
			"host": target.Addr,
		})
		ro.Host = target.Addr

		return h.forwardUnix(ctx, conn, target, ro, log)
	}

	cc, err := h.options.Router.Dial(ctx, "tcp", "@")
	if err != nil {
		log.Error(err)
		return err
	}
	defer cc.Close()

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
			Recorder:           h.recorder.Recorder,
			RecorderOptions:    h.recorder.Options,
			Certificate:        h.md.certificate,
			PrivateKey:         h.md.privateKey,
			NegotiatedProtocol: h.md.alpn,
			CertPool:           h.certPool,
			MitmBypass:         h.md.mitmBypass,
			ReadTimeout:        h.md.readTimeout,
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
	log.Infof("%s <-> %s", conn.LocalAddr(), "@")
	// xnet.Transport(conn, cc)
	xnet.Pipe(ctx, conn, cc)
	log.WithFields(map[string]any{
		"duration": time.Since(t),
	}).Infof("%s >-< %s", conn.LocalAddr(), "@")

	return nil
}

func (h *unixHandler) forwardUnix(ctx context.Context, conn net.Conn, target *chain.Node, ro *xrecorder.HandlerRecorderObject, log logger.Logger) (err error) {
	log.Debugf("%s >> %s", conn.LocalAddr(), target.Addr)
	var cc io.ReadWriteCloser

	if opts := h.options.Router.Options(); opts != nil && opts.Chain != nil {
		cc, err = h.options.Router.Dial(ctx, "unix", target.Addr)
	} else {
		cc, err = (&net.Dialer{}).DialContext(ctx, "unix", target.Addr)
	}
	if err != nil {
		log.Error(err)
		return err
	}
	defer cc.Close()

	var rw io.ReadWriteCloser = conn
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

		rw = xio.NewReadWriteCloser(br, conn, conn)
		switch proto {
		case sniffing.ProtoHTTP:
			ro2 := &xrecorder.HandlerRecorderObject{}
			*ro2 = *ro
			ro.Time = time.Time{}
			return h.handleHTTP(ctx, rw, cc, ro2, log)
		case sniffing.ProtoTLS:
			return h.handleTLS(ctx, rw, cc, ro, log)
		}
	}

	t := time.Now()
	log.Infof("%s <-> %s", conn.LocalAddr(), target.Addr)
	// xnet.Transport(rw, cc)
	xnet.Pipe(ctx, rw, cc)
	log.WithFields(map[string]any{
		"duration": time.Since(t),
	}).Infof("%s >-< %s", conn.LocalAddr(), target.Addr)

	return nil
}
