package tls

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"github.com/dolfly/core/dialer"
	"github.com/dolfly/core/logger"
	md "github.com/dolfly/core/metadata"
	xctx "github.com/dolfly/x/ctx"
	xnet "github.com/dolfly/x/internal/net"
	"github.com/dolfly/x/internal/net/proxyproto"
	"github.com/dolfly/x/registry"
)

func init() {
	registry.DialerRegistry().Register("tls", NewDialer)
}

type tlsDialer struct {
	md      metadata
	log     logger.Logger
	options dialer.Options
}

func NewDialer(opts ...dialer.Option) dialer.Dialer {
	options := dialer.Options{}
	for _, opt := range opts {
		opt(&options)
	}

	return &tlsDialer{
		log:     options.Logger,
		options: options,
	}
}

func (d *tlsDialer) Init(md md.Metadata) (err error) {
	if err = d.parseMetadata(md); err != nil {
		return err
	}

	return d.md.ech.Init(d.log, d.options.TLSConfig)
}

func (d *tlsDialer) Dial(ctx context.Context, addr string, opts ...dialer.DialOption) (net.Conn, error) {
	var options dialer.DialOptions
	for _, opt := range opts {
		opt(&options)
	}

	conn, err := options.Dialer.Dial(ctx, "tcp", addr)
	if err != nil {
		if d.log != nil {
			d.log.Error(err)
		}
	}

	xnet.ApplyNoDelay(conn)

	if d.md.keepalive {
		xnet.ApplyKeepalive(conn, net.KeepAliveConfig{
			Enable:   true,
			Idle:     d.md.keepaliveIdle,
			Interval: d.md.keepaliveInterval,
			Count:    d.md.keepaliveCount,
		})
	}

	conn = proxyproto.WrapClientConn(
		d.options.ProxyProtocol,
		xctx.SrcAddrFromContext(ctx),
		xctx.DstAddrFromContext(ctx),
		conn)

	return conn, err
}

// Handshake implements dialer.Handshaker
func (d *tlsDialer) Handshake(ctx context.Context, conn net.Conn, options ...dialer.HandshakeOption) (net.Conn, error) {
	if d.md.handshakeTimeout > 0 {
		conn.SetDeadline(time.Now().Add(d.md.handshakeTimeout))
		defer conn.SetDeadline(time.Time{})
	}

	cfg := d.options.TLSConfig
	serverName := ""
	if cfg != nil {
		serverName = cfg.ServerName
	}

	cfg, err := d.md.ech.Apply(ctx, cfg, serverName)
	if err != nil {
		if conn != nil {
			conn.Close()
		}
		return nil, err
	}

	tlsConn := tls.Client(conn, cfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		if conn != nil {
			conn.Close()
		}
		if rerr := d.md.ech.OnRejection(err, serverName); rerr != nil {
			return nil, rerr
		}
		return nil, err
	}

	return tlsConn, nil
}
