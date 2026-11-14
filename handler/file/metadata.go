package file

import (
	mdata "github.com/dolfly/core/metadata"
	mdutil "github.com/dolfly/x/metadata/util"
)

type metadata struct {
	dir string
	put bool
}

func (h *fileHandler) parseMetadata(md mdata.Metadata) (err error) {
	h.md.dir = mdutil.GetString(md, "file.dir", "dir")
	h.md.put = mdutil.GetBool(md, "file.put", "put")
	return
}
