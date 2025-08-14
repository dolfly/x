package ss

import (
	"bytes"
	"context"
	"errors"
	"net"
	"time"

	"github.com/dolfly/core/common/bufpool"
	"github.com/dolfly/core/handler"
	"github.com/dolfly/core/logger"
	md "github.com/dolfly/core/metadata"
	"github.com/dolfly/core/recorder"
	xctx "github.com/dolfly/x/ctx"
	ictx "github.com/dolfly/x/internal/ctx"
	"github.com/dolfly/x/internal/util/relay"
	"github.com/dolfly/x/internal/util/ss"
	rate_limiter "github.com/dolfly/x/limiter/rate"
	xrecorder "github.com/dolfly/x/recorder"
	"github.com/dolfly/x/registry"
	"github.com/shadowsocks/go-shadowsocks2/core"
)

func init() {
	registry.HandlerRegistry().Register("ssu", NewHandler)
}

type ssuHandler struct {
	cipher   core.Cipher
	md       metadata
	options  handler.Options
	recorder recorder.RecorderObject
}

func NewHandler(opts ...handler.Option) handler.Handler {
	options := handler.Options{}
	for _, opt := range opts {
		opt(&options)
	}

	return &ssuHandler{
		options: options,
	}
}

func (h *ssuHandler) Init(md md.Metadata) (err error) {
	if err = h.parseMetadata(md); err != nil {
		return
	}

	if h.options.Auth != nil {
		method := h.options.Auth.Username()
		password, _ := h.options.Auth.Password()
		h.cipher, err = ss.ShadowCipher(method, password, h.md.key)
		if err != nil {
			return
		}
	}

	for _, ro := range h.options.Recorders {
		if ro.Record == xrecorder.RecorderServiceHandler {
			h.recorder = ro
			break
		}
	}

	return
}

func (h *ssuHandler) Handle(ctx context.Context, conn net.Conn, opts ...handler.HandleOption) (err error) {
	defer conn.Close()

	start := time.Now()

	ro := &xrecorder.HandlerRecorderObject{
		Network:    "udp",
		Service:    h.options.Service,
		RemoteAddr: conn.RemoteAddr().String(),
		LocalAddr:  conn.LocalAddr().String(),
		SID:        xctx.SidFromContext(ctx).String(),
		Time:       start,
	}

	if srcAddr := xctx.SrcAddrFromContext(ctx); srcAddr != nil {
		ro.ClientAddr = srcAddr.String()
	}

	log := h.options.Logger.WithFields(map[string]any{
		"network": ro.Network,
		"remote":  conn.RemoteAddr().String(),
		"local":   conn.LocalAddr().String(),
		"client":  ro.ClientAddr,
		"sid":     ro.SID,
	})

	log.Infof("%s <> %s", conn.RemoteAddr(), conn.LocalAddr())
	defer func() {
		if err != nil {
			ro.Err = err.Error()
		}
		ro.Duration = time.Since(start)
		ro.Record(ctx, h.recorder.Recorder)

		log.WithFields(map[string]any{
			"duration": time.Since(start),
		}).Infof("%s >< %s", conn.RemoteAddr(), conn.LocalAddr())
	}()

	if !h.checkRateLimit(conn.RemoteAddr()) {
		return rate_limiter.ErrRateLimit
	}

	pc, ok := conn.(net.PacketConn)
	if ok {
		if h.cipher != nil {
			pc = h.cipher.PacketConn(pc)
		}
		// standard UDP relay.
		pc = ss.UDPServerConn(pc, conn.RemoteAddr(), h.md.udpBufferSize)
	} else {
		if h.cipher != nil {
			conn = ss.ShadowConn(h.cipher.StreamConn(conn), nil)
		}
		// UDP over TCP
		pc = relay.UDPTunServerConn(conn)
	}

	// obtain a udp connection
	var buf bytes.Buffer
	c, err := h.options.Router.Dial(ictx.ContextWithBuffer(ctx, &buf), "udp", "") // UDP association
	ro.Route = buf.String()
	if err != nil {
		log.Error(err)
		return err
	}
	defer c.Close()

	cc, ok := c.(net.PacketConn)
	if !ok {
		err := errors.New("ss: wrong connection type")
		log.Error(err)
		return err
	}

	t := time.Now()
	log.Infof("%s <-> %s", conn.LocalAddr(), cc.LocalAddr())
	h.relayPacket(ctx, pc, cc, ro, log)
	log.WithFields(map[string]any{"duration": time.Since(t)}).
		Infof("%s >-< %s", conn.LocalAddr(), cc.LocalAddr())

	return nil
}

func (h *ssuHandler) relayPacket(ctx context.Context, pc1, pc2 net.PacketConn, ro *xrecorder.HandlerRecorderObject, log logger.Logger) (err error) {
	errc := make(chan error, 2)

	bufferSize := h.md.udpBufferSize
	if bufferSize <= 0 {
		bufferSize = defaultBufferSize
	}

	go func() {
		b := bufpool.Get(bufferSize)
		defer bufpool.Put(b)

		for {
			err := func() error {
				n, addr, err := pc1.ReadFrom(b)
				if err != nil {
					return err
				}

				if ro.Host == "" {
					ro.Host = addr.String()
				}

				if h.options.Bypass != nil && h.options.Bypass.Contains(ctx, addr.Network(), addr.String()) {
					log.Warn("bypass: ", addr)
					return nil
				}

				if _, err = pc2.WriteTo(b[:n], addr); err != nil {
					return err
				}

				log.Tracef("%s >>> %s data: %d",
					pc2.LocalAddr(), addr, n)
				return nil
			}()

			if err != nil {
				errc <- err
				return
			}
		}
	}()

	go func() {
		b := bufpool.Get(bufferSize)
		defer bufpool.Put(b)

		for {
			err := func() error {
				n, raddr, err := pc2.ReadFrom(b)
				if err != nil {
					return err
				}

				if h.options.Bypass != nil && h.options.Bypass.Contains(ctx, raddr.Network(), raddr.String()) {
					log.Warn("bypass: ", raddr)
					return nil
				}

				if _, err = pc1.WriteTo(b[:n], raddr); err != nil {
					return err
				}

				log.Tracef("%s <<< %s data: %d",
					pc2.LocalAddr(), raddr, n)
				return nil
			}()

			if err != nil {
				errc <- err
				return
			}
		}
	}()

	return <-errc
}

func (h *ssuHandler) checkRateLimit(addr net.Addr) bool {
	if h.options.RateLimiter == nil {
		return true
	}
	host, _, _ := net.SplitHostPort(addr.String())
	if limiter := h.options.RateLimiter.Limiter(host); limiter != nil {
		return limiter.Allow(1)
	}

	return true
}
