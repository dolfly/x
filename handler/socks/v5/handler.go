package v5

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/dolfly/core/handler"
	"github.com/dolfly/core/limiter"
	"github.com/dolfly/core/limiter/traffic"
	md "github.com/dolfly/core/metadata"
	"github.com/dolfly/core/observer"
	"github.com/dolfly/core/observer/stats"
	"github.com/dolfly/core/recorder"
	"github.com/dolfly/gosocks5"
	xctx "github.com/dolfly/x/ctx"
	"github.com/dolfly/x/internal/util/socks"
	stats_util "github.com/dolfly/x/internal/util/stats"
	tls_util "github.com/dolfly/x/internal/util/tls"
	rate_limiter "github.com/dolfly/x/limiter/rate"
	cache_limiter "github.com/dolfly/x/limiter/traffic/cache"
	xstats "github.com/dolfly/x/observer/stats"
	stats_wrapper "github.com/dolfly/x/observer/stats/wrapper"
	xrecorder "github.com/dolfly/x/recorder"
	"github.com/dolfly/x/registry"
)

var (
	ErrUnknownCmd = errors.New("socks5: unknown command")
)

func init() {
	registry.HandlerRegistry().Register("socks5", NewHandler)
	registry.HandlerRegistry().Register("socks", NewHandler)
}

type socks5Handler struct {
	selector gosocks5.Selector
	md       metadata
	options  handler.Options
	stats    *stats_util.HandlerStats
	limiter  traffic.TrafficLimiter
	cancel   context.CancelFunc
	recorder recorder.RecorderObject
	certPool tls_util.CertPool
}

func NewHandler(opts ...handler.Option) handler.Handler {
	options := handler.Options{}
	for _, opt := range opts {
		opt(&options)
	}

	return &socks5Handler{
		options: options,
	}
}

func (h *socks5Handler) Init(md md.Metadata) (err error) {
	if err = h.parseMetadata(md); err != nil {
		return
	}

	h.selector = &serverSelector{
		Authenticator: h.options.Auther,
		TLSConfig:     h.options.TLSConfig,
		logger:        h.options.Logger,
		noTLS:         h.md.noTLS,
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel

	if h.options.Observer != nil {
		h.stats = stats_util.NewHandlerStats(h.options.Service, h.md.observerResetTraffic)
		go h.observeStats(ctx)
	}

	if h.options.Limiter != nil {
		h.limiter = cache_limiter.NewCachedTrafficLimiter(h.options.Limiter,
			cache_limiter.RefreshIntervalOption(h.md.limiterRefreshInterval),
			cache_limiter.CleanupIntervalOption(h.md.limiterCleanupInterval),
			cache_limiter.ScopeOption(limiter.ScopeClient),
		)
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

func (h *socks5Handler) Handle(ctx context.Context, conn net.Conn, opts ...handler.HandleOption) (err error) {
	defer conn.Close()

	start := time.Now()

	ro := &xrecorder.HandlerRecorderObject{
		Network:    "tcp",
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

	pStats := xstats.Stats{}
	conn = stats_wrapper.WrapConn(conn, &pStats)

	defer func() {
		if err != nil {
			ro.Err = err.Error()
		}
		ro.InputBytes += pStats.Get(stats.KindInputBytes)
		ro.OutputBytes += pStats.Get(stats.KindOutputBytes)
		ro.Duration = time.Since(start)
		if err := ro.Record(ctx, h.recorder.Recorder); err != nil {
			log.Errorf("record: %v", err)
		}

		log.WithFields(map[string]any{
			"network":     ro.Network,
			"duration":    time.Since(start),
			"inputBytes":  ro.InputBytes,
			"outputBytes": ro.OutputBytes,
		}).Infof("%s >< %s", conn.RemoteAddr(), conn.LocalAddr())
	}()

	if !h.checkRateLimit(conn.RemoteAddr()) {
		return rate_limiter.ErrRateLimit
	}

	conn.SetReadDeadline(time.Now().Add(h.md.readTimeout))

	sc := gosocks5.ServerConn(conn, h.selector)
	req, err := gosocks5.ReadRequest(sc)
	if err != nil {
		log.Error(err)
		return err
	}
	log.Trace(req)

	if clientID := sc.ID(); clientID != "" {
		ctx = xctx.ContextWithClientID(ctx, xctx.ClientID(clientID))
		log = log.WithFields(map[string]any{"user": clientID, "clientID": clientID})
		ro.ClientID = clientID
	}

	conn = sc
	conn.SetReadDeadline(time.Time{})

	address := req.Addr.String()
	ro.Host = address

	switch req.Cmd {
	case gosocks5.CmdConnect:
		return h.handleConnect(ctx, conn, "tcp", address, ro, log)
	case gosocks5.CmdBind:
		return h.handleBind(ctx, conn, "tcp", address, ro, log)
	case socks.CmdMuxBind:
		return h.handleMuxBind(ctx, conn, "tcp", address, ro, log)
	case gosocks5.CmdUdp:
		ro.Network = "udp"
		return h.handleUDP(ctx, conn, "udp", ro, log)
	case socks.CmdUDPTun:
		ro.Network = "udp"
		return h.handleUDPTun(ctx, conn, "udp", address, ro, log)
	default:
		err = ErrUnknownCmd
		log.Error(err)
		resp := gosocks5.NewReply(gosocks5.CmdUnsupported, nil)
		log.Trace(resp)
		resp.Write(conn)
		return err
	}
}

func (h *socks5Handler) Close() error {
	if h.cancel != nil {
		h.cancel()
	}
	return nil
}

func (h *socks5Handler) checkRateLimit(addr net.Addr) bool {
	if h.options.RateLimiter == nil {
		return true
	}
	host, _, _ := net.SplitHostPort(addr.String())
	if limiter := h.options.RateLimiter.Limiter(host); limiter != nil {
		return limiter.Allow(1)
	}

	return true
}

func (h *socks5Handler) observeStats(ctx context.Context) {
	if h.options.Observer == nil {
		return
	}

	var events []observer.Event

	ticker := time.NewTicker(h.md.observerPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if len(events) > 0 {
				if err := h.options.Observer.Observe(ctx, events); err == nil {
					events = nil
				}
				break
			}

			evs := h.stats.Events()
			if err := h.options.Observer.Observe(ctx, evs); err != nil {
				events = evs
			}

		case <-ctx.Done():
			return
		}
	}
}
