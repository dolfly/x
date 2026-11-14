package v5

import (
	"net"

	md "github.com/dolfly/core/metadata"
	"github.com/dolfly/gosocks5"
	xmd "github.com/dolfly/x/metadata"
)

// resolveConn wraps a raw net.Conn and carries the resolved address from a
// Tor RESOLVE/RESOLVE_PTR reply as metadata. It implements net.Conn (passthrough)
// and metadata.Metadatable so the handler can extract the resolved address.
type resolveConn struct {
	net.Conn
	resolvedAddr *gosocks5.Addr
}

func (c *resolveConn) Metadata() md.Metadata {
	return xmd.NewMetadata(map[string]any{
		"resolvedAddr": c.resolvedAddr,
	})
}
