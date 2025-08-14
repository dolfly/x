package api

import (
	mdata "github.com/dolfly/core/metadata"
	mdutil "github.com/dolfly/x/metadata/util"
)

type metadata struct {
	accesslog  bool
	pathPrefix string
}

func (h *apiHandler) parseMetadata(md mdata.Metadata) (err error) {
	h.md.accesslog = mdutil.GetBool(md, "api.accessLog", "accessLog")
	h.md.pathPrefix = mdutil.GetString(md, "api.pathPrefix", "pathPrefix")
	return
}
