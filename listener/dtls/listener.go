package dtls

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"github.com/dolfly/core/limiter"
	"github.com/dolfly/core/listener"
	"github.com/dolfly/core/logger"
	md "github.com/dolfly/core/metadata"
	admission "github.com/dolfly/x/admission/wrapper"
	xnet "github.com/dolfly/x/internal/net"
	"github.com/dolfly/x/internal/net/proxyproto"
	xdtls "github.com/dolfly/x/internal/util/dtls"
	climiter "github.com/dolfly/x/limiter/conn/wrapper"
	limiter_wrapper "github.com/dolfly/x/limiter/traffic/wrapper"
	metrics "github.com/dolfly/x/metrics/wrapper"
	stats "github.com/dolfly/x/observer/stats/wrapper"
	"github.com/dolfly/x/registry"
	"github.com/pion/dtls/v2"
)

func init() {
	registry.ListenerRegistry().Register("dtls", NewListener)
}

type dtlsListener struct {
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
	return &dtlsListener{
		logger:  options.Logger,
		options: options,
	}
}

func (l *dtlsListener) Init(md md.Metadata) (err error) {
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

	tlsCfg := l.options.TLSConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{}
	}
	config := dtls.Config{
		Certificates:         tlsCfg.Certificates,
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
		// Create timeout context for accepted connection.
		ConnectContextMaker: func() (context.Context, func()) {
			return context.WithTimeout(context.Background(), 30*time.Second)
		},
		ClientCAs:      tlsCfg.ClientCAs,
		ClientAuth:     dtls.ClientAuthType(tlsCfg.ClientAuth),
		FlightInterval: l.md.flightInterval,
		MTU:            l.md.mtu,
	}

	ln, err := dtls.Listen(network, laddr, &config)
	if err != nil {
		return
	}
	ln = proxyproto.WrapListener(l.options.ProxyProtocol, ln, 10*time.Second)
	ln = metrics.WrapListener(l.options.Service, ln)
	ln = stats.WrapListener(ln, l.options.Stats)
	ln = admission.WrapListener(l.options.Admission, ln)
	ln = limiter_wrapper.WrapListener(l.options.Service, ln, l.options.TrafficLimiter)
	ln = climiter.WrapListener(l.options.ConnLimiter, ln)

	l.ln = ln

	return
}

func (l *dtlsListener) Accept() (conn net.Conn, err error) {
	c, err := l.ln.Accept()
	if err != nil {
		return
	}
	conn = xdtls.Conn(c, l.md.bufferSize)
	conn = limiter_wrapper.WrapConn(
		conn,
		l.options.TrafficLimiter,
		conn.RemoteAddr().String(),
		limiter.ScopeOption(limiter.ScopeConn),
		limiter.ServiceOption(l.options.Service),
		limiter.NetworkOption(conn.LocalAddr().Network()),
		limiter.SrcOption(conn.RemoteAddr().String()),
	)

	return
}

func (l *dtlsListener) Addr() net.Addr {
	return l.ln.Addr()
}

func (l *dtlsListener) Close() error {
	return l.ln.Close()
}
