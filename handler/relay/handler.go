package relay

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/dolfly/core/handler"
	"github.com/dolfly/core/hop"
	"github.com/dolfly/core/limiter"
	"github.com/dolfly/core/limiter/traffic"
	md "github.com/dolfly/core/metadata"
	"github.com/dolfly/core/observer"
	"github.com/dolfly/core/observer/stats"
	"github.com/dolfly/core/recorder"
	"github.com/dolfly/relay"
	xctx "github.com/dolfly/x/ctx"
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
	ErrBadVersion   = errors.New("relay: bad version")
	ErrUnknownCmd   = errors.New("relay: unknown command")
	ErrUnauthorized = errors.New("relay: unauthorized")
)

func init() {
	registry.HandlerRegistry().Register("relay", NewHandler)
}

type relayHandler struct {
	hop      hop.Hop
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

	return &relayHandler{
		options: options,
	}
}

func (h *relayHandler) Init(md md.Metadata) (err error) {
	if err := h.parseMetadata(md); err != nil {
		return err
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

	return nil
}

// Forward implements handler.Forwarder.
func (h *relayHandler) Forward(hop hop.Hop) {
	h.hop = hop
}

func (h *relayHandler) Handle(ctx context.Context, conn net.Conn, opts ...handler.HandleOption) (err error) {
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

	if !h.checkRateLimit(conn.RemoteAddr()) {
		return rate_limiter.ErrRateLimit
	}

	if h.md.readTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(h.md.readTimeout))
	}

	req := relay.Request{}
	if _, err := req.ReadFrom(conn); err != nil {
		return err
	}

	conn.SetReadDeadline(time.Time{})

	resp := relay.Response{
		Version: relay.Version1,
		Status:  relay.StatusOK,
	}

	if req.Version != relay.Version1 {
		resp.Status = relay.StatusBadRequest
		resp.WriteTo(conn)
		return ErrBadVersion
	}

	var user, pass string
	var address string
	var networkID relay.NetworkID
	for _, f := range req.Features {
		switch f.Type() {
		case relay.FeatureUserAuth:
			if feature, _ := f.(*relay.UserAuthFeature); feature != nil {
				user, pass = feature.Username, feature.Password
			}
		case relay.FeatureAddr:
			if feature, _ := f.(*relay.AddrFeature); feature != nil {
				address = net.JoinHostPort(feature.Host, strconv.Itoa(int(feature.Port)))
			}
		case relay.FeatureNetwork:
			if feature, _ := f.(*relay.NetworkFeature); feature != nil {
				networkID = feature.Network
			}
		}
	}

	if user != "" {
		ro.ClientID = user
		log = log.WithFields(map[string]any{"user": user})
	}

	if h.options.Auther != nil {
		clientID, ok := h.options.Auther.Authenticate(ctx, user, pass)
		if !ok {
			resp.Status = relay.StatusUnauthorized
			resp.WriteTo(conn)
			return ErrUnauthorized
		}
		log = log.WithFields(map[string]any{"clientID": clientID})
		ro.ClientID = clientID
		ctx = xctx.ContextWithClientID(ctx, xctx.ClientID(clientID))
	}

	network := networkID.String()
	if (req.Cmd & relay.FUDP) == relay.FUDP {
		network = "udp"
	}
	ro.Network = network
	ro.Host = address
	log = log.WithFields(map[string]any{"network": network})

	if h.hop != nil {
		// forward mode
		return h.handleForward(ctx, conn, network, ro, log)
	}

	switch req.Cmd & relay.CmdMask {
	case 0, relay.CmdConnect:
		return h.handleConnect(ctx, conn, network, address, ro, log)
	case relay.CmdBind:
		return h.handleBind(ctx, conn, network, address, ro, log)
	default:
		resp.Status = relay.StatusBadRequest
		resp.WriteTo(conn)
		return ErrUnknownCmd
	}
}

// Close implements io.Closer interface.
func (h *relayHandler) Close() error {
	if h.cancel != nil {
		h.cancel()
	}
	return nil
}

func (h *relayHandler) checkRateLimit(addr net.Addr) bool {
	if h.options.RateLimiter == nil {
		return true
	}
	host, _, _ := net.SplitHostPort(addr.String())
	if limiter := h.options.RateLimiter.Limiter(host); limiter != nil {
		return limiter.Allow(1)
	}

	return true
}

func (h *relayHandler) observeStats(ctx context.Context) {
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
