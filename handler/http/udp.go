package http

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/dolfly/core/logger"
	ictx "github.com/dolfly/x/internal/ctx"
	"github.com/dolfly/x/internal/net/udp"
	"github.com/dolfly/x/internal/util/socks"
	xrecorder "github.com/dolfly/x/recorder"
)

func (h *httpHandler) handleUDP(ctx context.Context, conn net.Conn, ro *xrecorder.HandlerRecorderObject, log logger.Logger) error {
	log = log.WithFields(map[string]any{
		"cmd": "udp",
	})

	resp := &http.Response{
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     h.md.header,
	}
	if resp.Header == nil {
		resp.Header = http.Header{}
	}

	if !h.md.enableUDP {
		resp.StatusCode = http.StatusForbidden

		if log.IsLevelEnabled(logger.TraceLevel) {
			dump, _ := httputil.DumpResponse(resp, false)
			log.Trace(string(dump))
		}

		log.Error("http: UDP relay is disabled")

		return resp.Write(conn)
	}

	resp.StatusCode = http.StatusOK
	if log.IsLevelEnabled(logger.TraceLevel) {
		dump, _ := httputil.DumpResponse(resp, false)
		log.Trace(string(dump))
	}
	if err := resp.Write(conn); err != nil {
		log.Error(err)
		return err
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

	log.WithFields(map[string]any{"src": c.LocalAddr().String()})
	ro.SrcAddr = c.LocalAddr().String()

	pc, ok := c.(net.PacketConn)
	if !ok {
		err = errors.New("wrong connection type")
		log.Error(err)
		return err
	}

	relay := udp.NewRelay(socks.UDPTunServerConn(conn), pc).
		WithBypass(h.options.Bypass).
		WithBufferSize(h.md.udpBufferSize).
		WithLogger(log)

	t := time.Now()
	log.Infof("%s <-> %s", conn.RemoteAddr(), pc.LocalAddr())
	relay.Run(ctx)
	log.WithFields(map[string]any{
		"duration": time.Since(t),
	}).Infof("%s >-< %s", conn.RemoteAddr(), pc.LocalAddr())

	return nil
}
