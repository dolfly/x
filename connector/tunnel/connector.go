package tunnel

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/dolfly/core/connector"
	md "github.com/dolfly/core/metadata"
	"github.com/dolfly/relay"
	xctx "github.com/dolfly/x/ctx"
	"github.com/dolfly/x/registry"
)

func init() {
	registry.ConnectorRegistry().Register("tunnel", NewConnector)
}

type tunnelConnector struct {
	md      metadata
	options connector.Options
}

func NewConnector(opts ...connector.Option) connector.Connector {
	options := connector.Options{}
	for _, opt := range opts {
		opt(&options)
	}

	return &tunnelConnector{
		options: options,
	}
}

func (c *tunnelConnector) Init(md md.Metadata) (err error) {
	return c.parseMetadata(md)
}

func (c *tunnelConnector) Connect(ctx context.Context, conn net.Conn, network, address string, opts ...connector.ConnectOption) (net.Conn, error) {
	log := c.options.Logger.WithFields(map[string]any{
		"network": network,
		"address": address,
		"remote":  conn.RemoteAddr().String(),
		"local":   conn.LocalAddr().String(),
		"sid":     xctx.SidFromContext(ctx).String(),
	})
	log.Debugf("connect %s/%s", address, network)

	if c.md.connectTimeout > 0 {
		conn.SetDeadline(time.Now().Add(c.md.connectTimeout))
		defer conn.SetDeadline(time.Time{})
	}

	req := relay.Request{
		Version: relay.Version1,
		Cmd:     relay.CmdConnect,
	}

	switch network {
	case "udp", "udp4", "udp6":
		req.Cmd |= relay.FUDP
		req.Features = append(req.Features, &relay.NetworkFeature{
			Network: relay.NetworkUDP,
		})
	}

	if c.options.Auth != nil {
		pwd, _ := c.options.Auth.Password()
		req.Features = append(req.Features, &relay.UserAuthFeature{
			Username: c.options.Auth.Username(),
			Password: pwd,
		})
	}

	srcAddr := conn.RemoteAddr().String()
	if v := xctx.SrcAddrFromContext(ctx); v != nil {
		srcAddr = v.String()
	}

	af := &relay.AddrFeature{}
	af.ParseFrom(srcAddr)
	req.Features = append(req.Features, af) // src address

	af = &relay.AddrFeature{}
	af.ParseFrom(address)
	req.Features = append(req.Features, af) // dst address

	req.Features = append(req.Features, &relay.TunnelFeature{
		ID: c.md.tunnelID,
	})

	if _, err := req.WriteTo(conn); err != nil {
		return nil, err
	}
	// drain the response
	if err := readResponse(conn); err != nil {
		return nil, err
	}

	switch network {
	case "tcp", "tcp4", "tcp6":
	case "udp", "udp4", "udp6":
		conn = &udpConn{
			Conn: conn,
		}
	default:
		err := fmt.Errorf("network %s is unsupported", network)
		log.Error(err)
		return nil, err
	}

	return conn, nil
}
