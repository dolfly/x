package ss

import (
	"time"

	mdata "github.com/dolfly/core/metadata"
	mdutil "github.com/dolfly/x/metadata/util"
)

type metadata struct {
	key            string
	connectTimeout time.Duration
	noDelay        bool
}

func (c *ssConnector) parseMetadata(md mdata.Metadata) (err error) {
	const (
		key            = "key"
		connectTimeout = "timeout"
		noDelay        = "nodelay"
	)

	c.md.key = mdutil.GetString(md, key)
	c.md.connectTimeout = mdutil.GetDuration(md, connectTimeout)
	c.md.noDelay = mdutil.GetBool(md, noDelay)

	return
}
