package udp

import (
	"context"
	"net"

	"github.com/dolfly/core/dialer"
	"github.com/dolfly/core/logger"
	md "github.com/dolfly/core/metadata"
	xctx "github.com/dolfly/x/ctx"
	"github.com/dolfly/x/internal/net/proxyproto"
	"github.com/dolfly/x/registry"
)

func init() {
	registry.DialerRegistry().Register("udp", NewDialer)
}

type udpDialer struct {
	md      metadata
	logger  logger.Logger
	options dialer.Options
}

func NewDialer(opts ...dialer.Option) dialer.Dialer {
	options := &dialer.Options{}
	for _, opt := range opts {
		opt(options)
	}

	return &udpDialer{
		logger:  options.Logger,
		options: *options,
	}
}

func (d *udpDialer) Init(md md.Metadata) (err error) {
	return d.parseMetadata(md)
}

func (d *udpDialer) Dial(ctx context.Context, addr string, opts ...dialer.DialOption) (net.Conn, error) {
	var options dialer.DialOptions
	for _, opt := range opts {
		opt(&options)
	}

	c, err := options.Dialer.Dial(ctx, "udp", addr)
	if err != nil {
		return nil, err
	}

	c = &conn{
		UDPConn: c.(*net.UDPConn),
	}

	c = proxyproto.WrapClientConn(
		d.options.ProxyProtocol,
		xctx.SrcAddrFromContext(ctx),
		xctx.DstAddrFromContext(ctx),
		c)

	return c, nil
}
