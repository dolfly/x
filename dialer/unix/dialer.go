package unix

import (
	"context"
	"net"

	"github.com/dolfly/core/dialer"
	"github.com/dolfly/core/logger"
	md "github.com/dolfly/core/metadata"
	"github.com/dolfly/x/registry"
)

func init() {
	registry.DialerRegistry().Register("unix", NewDialer)
}

type unixDialer struct {
	md     metadata
	logger logger.Logger
}

func NewDialer(opts ...dialer.Option) dialer.Dialer {
	options := &dialer.Options{}
	for _, opt := range opts {
		opt(options)
	}

	return &unixDialer{
		logger: options.Logger,
	}
}

func (d *unixDialer) Init(md md.Metadata) (err error) {
	return d.parseMetadata(md)
}

func (d *unixDialer) Dial(ctx context.Context, addr string, opts ...dialer.DialOption) (net.Conn, error) {
	var options dialer.DialOptions
	for _, opt := range opts {
		opt(&options)
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", addr)
	if err != nil {
		d.logger.Error(err)
	}
	return conn, err
}
