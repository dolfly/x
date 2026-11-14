package masque

import (
	"time"

	mdata "github.com/dolfly/core/metadata"
	mdutil "github.com/dolfly/x/metadata/util"
)

type metadata struct {
	connectTimeout time.Duration
}

func (c *masqueConnector) parseMetadata(md mdata.Metadata) (err error) {
	c.md.connectTimeout = mdutil.GetDuration(md, "timeout", "connectTimeout")
	return nil
}
