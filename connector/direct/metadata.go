package direct

import (
	"strings"

	mdata "github.com/dolfly/core/metadata"
	mdutil "github.com/dolfly/x/metadata/util"
)

type metadata struct {
	action string
}

func (c *directConnector) parseMetadata(md mdata.Metadata) (err error) {
	c.md.action = strings.ToLower(mdutil.GetString(md, "action"))
	return
}
