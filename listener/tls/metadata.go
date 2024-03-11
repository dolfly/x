package tls

import (
	mdata "github.com/dolfly/core/metadata"
	mdutil "github.com/dolfly/core/metadata/util"
)

type metadata struct {
	mptcp bool
}

func (l *tlsListener) parseMetadata(md mdata.Metadata) (err error) {
	l.md.mptcp = mdutil.GetBool(md, "mptcp")
	return
}
