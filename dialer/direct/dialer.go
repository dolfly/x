package direct

import (
	"context"
	"net"

	"github.com/dolfly/core/dialer"
	"github.com/dolfly/core/logger"
	md "github.com/dolfly/core/metadata"
	"github.com/dolfly/x/registry"
)

func init() {
	registry.DialerRegistry().Register("direct", NewDialer)
	registry.DialerRegistry().Register("virtual", NewDialer)
}

type directDialer struct {
	logger logger.Logger
}

func NewDialer(opts ...dialer.Option) dialer.Dialer {
	options := &dialer.Options{}
	for _, opt := range opts {
		opt(options)
	}

	return &directDialer{
		logger: options.Logger,
	}
}

func (d *directDialer) Init(md md.Metadata) (err error) {
	return
}

func (d *directDialer) Dial(ctx context.Context, addr string, opts ...dialer.DialOption) (net.Conn, error) {
	return &conn{}, nil
}
