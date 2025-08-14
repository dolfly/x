package tcp

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
	registry.DialerRegistry().Register("tcp", NewDialer)
}

type tcpDialer struct {
	md      metadata
	logger  logger.Logger
	options dialer.Options
}

func NewDialer(opts ...dialer.Option) dialer.Dialer {
	options := dialer.Options{}
	for _, opt := range opts {
		opt(&options)
	}

	return &tcpDialer{
		logger:  options.Logger,
		options: options,
	}
}

func (d *tcpDialer) Init(md md.Metadata) (err error) {
	return d.parseMetadata(md)
}

func (d *tcpDialer) Dial(ctx context.Context, addr string, opts ...dialer.DialOption) (net.Conn, error) {
	var options dialer.DialOptions
	for _, opt := range opts {
		opt(&options)
	}

	conn, err := options.Dialer.Dial(ctx, "tcp", addr)
	if err != nil {
		d.logger.Error(err)
	}

	conn = proxyproto.WrapClientConn(
		d.options.ProxyProtocol,
		xctx.SrcAddrFromContext(ctx),
		xctx.DstAddrFromContext(ctx),
		conn)

	return conn, err
}
