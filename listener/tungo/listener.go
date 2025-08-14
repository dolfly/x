package tungo

import (
	"context"
	"net"
	"time"

	"github.com/dolfly/core/limiter"
	"github.com/dolfly/core/listener"
	"github.com/dolfly/core/logger"
	mdata "github.com/dolfly/core/metadata"
	"github.com/dolfly/core/router"
	ictx "github.com/dolfly/x/internal/ctx"
	traffic_limiter "github.com/dolfly/x/limiter/traffic"
	limiter_wrapper "github.com/dolfly/x/limiter/traffic/wrapper"
	mdx "github.com/dolfly/x/metadata"
	metrics "github.com/dolfly/x/metrics/wrapper"
	stats "github.com/dolfly/x/observer/stats/wrapper"
	"github.com/dolfly/x/registry"
)

func init() {
	registry.ListenerRegistry().Register("tungo", NewListener)
}

type tunListener struct {
	addr    net.Addr
	cqueue  chan net.Conn
	closed  chan struct{}
	log     logger.Logger
	md      metadata
	options listener.Options
	routes  []*router.Route
}

func NewListener(opts ...listener.Option) listener.Listener {
	options := listener.Options{}
	for _, opt := range opts {
		opt(&options)
	}
	return &tunListener{
		log:     options.Logger,
		options: options,
	}
}

func (l *tunListener) Init(md mdata.Metadata) (err error) {
	if err = l.parseMetadata(md); err != nil {
		return
	}

	l.addr = &Addr{Name: l.options.Addr}

	l.cqueue = make(chan net.Conn, 1)
	l.closed = make(chan struct{})

	ctx, done := context.WithCancelCause(context.Background())
	go l.listenLoop(done)

	<-ctx.Done()
	if err := context.Cause(ctx); err != ctx.Err() {
		return err
	}

	return
}

func (l *tunListener) listenLoop(ready context.CancelCauseFunc) {
	for {
		ctx, cancel := context.WithCancel(context.Background())
		err := func() error {
			ifce, name, ip, err := l.createTun()
			if err != nil {
				if ifce != nil {
					ifce.Close()
				}
				return err
			}

			itf, err := net.InterfaceByName(name)
			if err != nil {
				return err
			}

			addrs, _ := itf.Addrs()
			l.log.Infof("name: %s, net: %s, mtu: %d, addrs: %s",
				itf.Name, ip, l.md.config.MTU, addrs)

			ctx = ictx.ContextWithMetadata(ctx, mdx.NewMetadata(map[string]any{
				"config": l.md.config,
			}))

			var c net.Conn
			c = &conn{
				ifce:   ifce,
				laddr:  l.addr,
				raddr:  &net.IPAddr{IP: ip},
				ctx:    ctx,
				cancel: cancel,
			}
			c = metrics.WrapConn(l.options.Service, c)
			c = stats.WrapConn(c, l.options.Stats)
			c = limiter_wrapper.WrapConn(
				c,
				l.options.TrafficLimiter,
				traffic_limiter.ServiceLimitKey,
				limiter.ScopeOption(limiter.ScopeService),
				limiter.ServiceOption(l.options.Service),
				limiter.NetworkOption(c.LocalAddr().Network()),
			)
			l.cqueue <- c

			return nil
		}()
		if err != nil {
			l.log.Error(err)
			cancel()
		}

		ready(err)

		select {
		case <-ctx.Done():
		case <-l.closed:
			return
		}

		time.Sleep(time.Second)
	}
}

func (l *tunListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.cqueue:
		return conn, nil
	case <-l.closed:
	}

	return nil, listener.ErrClosed
}

func (l *tunListener) Addr() net.Addr {
	return l.addr
}

func (l *tunListener) Close() error {
	select {
	case <-l.closed:
		return net.ErrClosed
	default:
		close(l.closed)
	}
	return nil
}

type Addr struct {
	Name string
}

func (a *Addr) Network() string {
	return "tun"
}

func (a *Addr) String() string {
	return a.Name
}
