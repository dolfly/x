package tap

import (
	"math"

	mdata "github.com/dolfly/core/metadata"
	mdutil "github.com/dolfly/x/metadata/util"
)

const (
	MaxMessageSize = math.MaxUint16
)

type metadata struct {
	key string
}

func (h *tapHandler) parseMetadata(md mdata.Metadata) (err error) {
	const (
		key = "key"
	)

	h.md.key = mdutil.GetString(md, key)
	return
}
