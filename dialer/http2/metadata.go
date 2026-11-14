package http2

import (
	mdata "github.com/dolfly/core/metadata"
	"github.com/dolfly/x/internal/util/ech"
)

type metadata struct {
	ech ech.Config
}

func (d *http2Dialer) parseMetadata(md mdata.Metadata) (err error) {
	d.md.ech = ech.FromMetadata(md)
	return
}
