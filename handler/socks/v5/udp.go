package v5

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/dolfly/core/limiter"
	"github.com/dolfly/core/logger"
	"github.com/dolfly/core/observer/stats"
	"github.com/dolfly/gosocks5"
	ctxvalue "github.com/dolfly/x/ctx"
	ictx "github.com/dolfly/x/internal/ctx"
	xnet "github.com/dolfly/x/internal/net"
	"github.com/dolfly/x/internal/net/udp"
	"github.com/dolfly/x/internal/util/socks"
	traffic_wrapper "github.com/dolfly/x/limiter/traffic/wrapper"
	metrics "github.com/dolfly/x/metrics/wrapper"
	xstats "github.com/dolfly/x/observer/stats"
	stats_wrapper "github.com/dolfly/x/observer/stats/wrapper"
	xrecorder "github.com/dolfly/x/recorder"
)

func (h *socks5Handler) handleUDP(ctx context.Context, conn net.Conn, network string, ro *xrecorder.HandlerRecorderObject, log logger.Logger) error {
	log = log.WithFields(map[string]any{
		"network": network,
		"cmd":     network,
	})

	if !h.md.enableUDP {
		reply := gosocks5.NewReply(gosocks5.NotAllowed, nil)
		log.Trace(reply)
		log.Error("socks5: UDP relay is disabled")
		return reply.Write(conn)
	}

	lc := xnet.ListenConfig{
		Netns: h.options.Netns,
	}

	cc, err := lc.ListenPacket(ctx, network, "")
	if err != nil {
		log.Error(err)
		reply := gosocks5.NewReply(gosocks5.Failure, nil)
		log.Trace(reply)
		reply.Write(conn)
		return err
	}
	defer cc.Close()

	log = log.WithFields(map[string]any{
		"src":  cc.LocalAddr().String(),
		"bind": cc.LocalAddr().String(),
	})
	ro.SrcAddr = cc.LocalAddr().String()

	saddr := gosocks5.Addr{}
	saddr.ParseFrom(cc.LocalAddr().String())

	saddr.Host, _, _ = net.SplitHostPort(conn.LocalAddr().String())
	if v := net.ParseIP(h.md.publicAddr); v != nil {
		saddr.Host = h.md.publicAddr
	}
	saddr.Type = 0
	reply := gosocks5.NewReply(gosocks5.Succeeded, &saddr)
	log.Trace(reply)
	if err := reply.Write(conn); err != nil {
		log.Error(err)
		return err
	}

	log.Debugf("bind on %s OK", cc.LocalAddr())

	// obtain a udp connection
	var buf bytes.Buffer
	c, err := h.options.Router.Dial(ictx.ContextWithBuffer(ctx, &buf), network, "") // UDP association
	ro.Route = buf.String()
	if err != nil {
		log.Error(err)
		return err
	}
	defer c.Close()

	pc, ok := c.(net.PacketConn)
	if !ok {
		err := errors.New("socks5: wrong connection type")
		log.Error(err)
		return err
	}
	pc = metrics.WrapPacketConn(ro.Service, pc)

	{
		pStats := xstats.Stats{}
		cc = stats_wrapper.WrapPacketConn(cc, &pStats)

		defer func() {
			ro.InputBytes = pStats.Get(stats.KindInputBytes)
			ro.OutputBytes = pStats.Get(stats.KindOutputBytes)
		}()

		clientID := ctxvalue.ClientIDFromContext(ctx)
		cc = traffic_wrapper.WrapPacketConn(
			cc,
			h.limiter,
			string(clientID),
			limiter.ServiceOption(h.options.Service),
			limiter.ScopeOption(limiter.ScopeClient),
			limiter.NetworkOption(network),
			limiter.ClientOption(string(clientID)),
			limiter.SrcOption(conn.RemoteAddr().String()),
		)
		if h.options.Observer != nil {
			pstats := h.stats.Stats(string(clientID))
			pstats.Add(stats.KindTotalConns, 1)
			pstats.Add(stats.KindCurrentConns, 1)
			defer pstats.Add(stats.KindCurrentConns, -1)
			cc = stats_wrapper.WrapPacketConn(cc, pstats)
		}
	}

	r := udp.NewRelay(socks.UDPConn(cc, h.md.udpBufferSize), pc).
		WithBypass(h.options.Bypass).
		WithBufferSize(h.md.udpBufferSize).
		WithLogger(log)

	go r.Run(ctx)

	t := time.Now()
	log.Debugf("%s <-> %s", conn.RemoteAddr(), cc.LocalAddr())
	io.Copy(io.Discard, conn)
	log.WithFields(map[string]any{"duration": time.Since(t)}).
		Debugf("%s >-< %s", conn.RemoteAddr(), cc.LocalAddr())

	return nil
}
