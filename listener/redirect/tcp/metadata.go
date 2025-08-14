package tcp

import (
	mdata "github.com/dolfly/core/metadata"
	mdutil "github.com/dolfly/x/metadata/util"
)

type metadata struct {
	tproxy bool
	mptcp  bool
}

func (l *redirectListener) parseMetadata(md mdata.Metadata) (err error) {
	l.md.tproxy = mdutil.GetBool(md, "tproxy")
	l.md.mptcp = mdutil.GetBool(md, "mptcp")

	return
}
