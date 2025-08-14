package h2

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/dolfly/core/dialer"
	"github.com/dolfly/core/logger"
	md "github.com/dolfly/core/metadata"
	xctx "github.com/dolfly/x/ctx"
	"github.com/dolfly/x/internal/net/proxyproto"
	"github.com/dolfly/x/registry"
	"golang.org/x/net/http2"
)

func init() {
	registry.DialerRegistry().Register("h2", NewTLSDialer)
	registry.DialerRegistry().Register("h2c", NewDialer)
}

type h2Dialer struct {
	clients     map[string]*http.Client
	clientMutex sync.Mutex
	h2c         bool
	logger      logger.Logger
	md          metadata
	options     dialer.Options
}

func NewDialer(opts ...dialer.Option) dialer.Dialer {
	options := dialer.Options{}
	for _, opt := range opts {
		opt(&options)
	}

	return &h2Dialer{
		h2c:     true,
		clients: make(map[string]*http.Client),
		logger:  options.Logger,
		options: options,
	}
}

func NewTLSDialer(opts ...dialer.Option) dialer.Dialer {
	options := dialer.Options{}
	for _, opt := range opts {
		opt(&options)
	}

	return &h2Dialer{
		clients: make(map[string]*http.Client),
		logger:  options.Logger,
		options: options,
	}
}

func (d *h2Dialer) Init(md md.Metadata) (err error) {
	if err = d.parseMetadata(md); err != nil {
		return
	}

	return nil
}

// Multiplex implements dialer.Multiplexer interface.
func (d *h2Dialer) Multiplex() bool {
	return true
}

func (d *h2Dialer) Dial(ctx context.Context, address string, opts ...dialer.DialOption) (net.Conn, error) {
	raddr, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		d.logger.Error(err)
		return nil, err
	}

	d.clientMutex.Lock()

	client, ok := d.clients[address]
	if !ok {
		options := &dialer.DialOptions{}
		for _, opt := range opts {
			opt(options)
		}

		client = &http.Client{}
		if d.h2c {
			client.Transport = &http2.Transport{
				AllowHTTP: true,
				DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
					conn, err := options.Dialer.Dial(ctx, network, addr)
					if err != nil {
						return nil, err
					}

					conn = proxyproto.WrapClientConn(
						d.options.ProxyProtocol,
						xctx.SrcAddrFromContext(ctx),
						xctx.DstAddrFromContext(ctx),
						conn)

					return conn, nil
				},
			}
		} else {
			client.Transport = &http.Transport{
				TLSClientConfig: d.options.TLSConfig,
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					conn, err := options.Dialer.Dial(ctx, network, addr)
					if err != nil {
						return nil, err
					}

					conn = proxyproto.WrapClientConn(
						d.options.ProxyProtocol,
						xctx.SrcAddrFromContext(ctx),
						xctx.DstAddrFromContext(ctx),
						conn)

					return conn, nil
				},
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			}
		}

		d.clients[address] = client
	}
	d.clientMutex.Unlock()

	host := d.md.host
	if host == "" {
		host = address
	}

	scheme := "https"
	if d.h2c {
		scheme = "http"
	}
	pr, pw := io.Pipe()
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Scheme: scheme, Host: host},
		Header: d.md.header,
		Body:   pr,
		Host:   host,
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if d.md.path != "" {
		req.Method = http.MethodGet
		req.URL.Path = d.md.path
	}

	if d.logger.IsLevelEnabled(logger.TraceLevel) {
		dump, _ := httputil.DumpRequest(req, false)
		d.logger.Trace(string(dump))
	}

	resp, err := client.Do(req.WithContext(context.WithoutCancel(ctx)))
	if err != nil {
		return nil, err
	}

	if d.logger.IsLevelEnabled(logger.TraceLevel) {
		dump, _ := httputil.DumpResponse(resp, false)
		d.logger.Trace(string(dump))
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, errors.New(resp.Status)
	}

	conn := &http2Conn{
		r:          resp.Body,
		w:          pw,
		remoteAddr: raddr,
		localAddr:  &net.TCPAddr{IP: net.IPv4zero, Port: 0},
	}
	return conn, nil
}
