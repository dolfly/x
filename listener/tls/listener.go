package tls

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"github.com/dolfly/core/listener"
	"github.com/dolfly/core/logger"
	md "github.com/dolfly/core/metadata"
	admission "github.com/dolfly/x/admission/wrapper"
	xnet "github.com/dolfly/x/internal/net"
	"github.com/dolfly/x/internal/net/proxyproto"
	climiter "github.com/dolfly/x/limiter/conn/wrapper"
	limiter "github.com/dolfly/x/limiter/traffic/wrapper"
	metrics "github.com/dolfly/x/metrics/wrapper"
	"github.com/dolfly/x/registry"
	stats "github.com/dolfly/x/stats/wrapper"
)

func init() {
	registry.ListenerRegistry().Register("tls", NewListener)
}

type tlsListener struct {
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
	return &tlsListener{
		logger:  options.Logger,
		options: options,
	}
}

func (l *tlsListener) Init(md md.Metadata) (err error) {
	if err = l.parseMetadata(md); err != nil {
		return
	}

	network := "tcp"
	if xnet.IsIPv4(l.options.Addr) {
		network = "tcp4"
	}

	lc := net.ListenConfig{}
	if l.md.mptcp {
		lc.SetMultipathTCP(true)
		l.logger.Debugf("mptcp enabled: %v", lc.MultipathTCP())
	}
	ln, err := lc.Listen(context.Background(), network, l.options.Addr)
	if err != nil {
		return
	}
	ln = proxyproto.WrapListener(l.options.ProxyProtocol, ln, 10*time.Second)
	ln = metrics.WrapListener(l.options.Service, ln)
	ln = stats.WrapListener(ln, l.options.Stats)
	ln = admission.WrapListener(l.options.Admission, ln)
	ln = limiter.WrapListener(l.options.TrafficLimiter, ln)
	ln = climiter.WrapListener(l.options.ConnLimiter, ln)

	l.ln = tls.NewListener(ln, l.options.TLSConfig)

	return
}

func (l *tlsListener) Accept() (conn net.Conn, err error) {
	return l.ln.Accept()
}

func (l *tlsListener) Addr() net.Addr {
	return l.ln.Addr()
}

func (l *tlsListener) Close() error {
	return l.ln.Close()
}
