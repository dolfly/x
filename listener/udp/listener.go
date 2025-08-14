package udp

import (
	"net"

	"github.com/dolfly/core/limiter"
	"github.com/dolfly/core/listener"
	"github.com/dolfly/core/logger"
	md "github.com/dolfly/core/metadata"
	admission "github.com/dolfly/x/admission/wrapper"
	xnet "github.com/dolfly/x/internal/net"
	"github.com/dolfly/x/internal/net/udp"
	traffic_limiter "github.com/dolfly/x/limiter/traffic"
	limiter_wrapper "github.com/dolfly/x/limiter/traffic/wrapper"
	metrics "github.com/dolfly/x/metrics/wrapper"
	stats "github.com/dolfly/x/observer/stats/wrapper"
	"github.com/dolfly/x/registry"
)

func init() {
	registry.ListenerRegistry().Register("udp", NewListener)
}

type udpListener struct {
	ln      net.Listener
	logger  logger.Logger
	md      metadata
	options listener.Options
}

func NewListener(opts ...listener.Option) listener.Listener {
	options := listener.Options{}
	for _, opt := range opts {
		opt(&options)
	}
	return &udpListener{
		logger:  options.Logger,
		options: options,
	}
}

func (l *udpListener) Init(md md.Metadata) (err error) {
	if err = l.parseMetadata(md); err != nil {
		return
	}

	network := "udp"
	if xnet.IsIPv4(l.options.Addr) {
		network = "udp4"
	}
	laddr, err := net.ResolveUDPAddr(network, l.options.Addr)
	if err != nil {
		return
	}

	var conn net.PacketConn
	conn, err = net.ListenUDP(network, laddr)
	if err != nil {
		return
	}
	conn = metrics.WrapPacketConn(l.options.Service, conn)
	conn = stats.WrapPacketConn(conn, l.options.Stats)
	conn = admission.WrapPacketConn(l.options.Admission, conn)
	conn = limiter_wrapper.WrapPacketConn(
		conn,
		l.options.TrafficLimiter,
		traffic_limiter.ServiceLimitKey,
		limiter.ScopeOption(limiter.ScopeService),
		limiter.ServiceOption(l.options.Service),
		limiter.NetworkOption(conn.LocalAddr().Network()),
	)

	l.ln = udp.NewListener(conn, &udp.ListenConfig{
		Backlog:        l.md.backlog,
		ReadQueueSize:  l.md.readQueueSize,
		ReadBufferSize: l.md.readBufferSize,
		Keepalive:      l.md.keepalive,
		TTL:            l.md.ttl,
		Logger:         l.logger,
	})
	return
}

func (l *udpListener) Accept() (conn net.Conn, err error) {
	return l.ln.Accept()
}

func (l *udpListener) Addr() net.Addr {
	return l.ln.Addr()
}

func (l *udpListener) Close() error {
	return l.ln.Close()
}
