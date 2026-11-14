package tls

import (
	"time"

	mdata "github.com/dolfly/core/metadata"
	"github.com/dolfly/x/internal/util/ech"
	mdutil "github.com/dolfly/x/metadata/util"
)

type metadata struct {
	handshakeTimeout time.Duration

	keepalive         bool
	keepaliveIdle     time.Duration
	keepaliveInterval time.Duration
	keepaliveCount    int

	ech ech.Config
}

func (d *tlsDialer) parseMetadata(md mdata.Metadata) (err error) {
	const (
		handshakeTimeout = "handshakeTimeout"
	)

	d.md.handshakeTimeout = mdutil.GetDuration(md, handshakeTimeout)

	d.md.keepalive = mdutil.GetBool(md, "keepalive")
	d.md.keepaliveIdle = mdutil.GetDuration(md, "keepalive.idle")
	d.md.keepaliveInterval = mdutil.GetDuration(md, "keepalive.interval")
	d.md.keepaliveCount = mdutil.GetInt(md, "keepalive.count")

	d.md.ech = ech.FromMetadata(md)

	return
}
