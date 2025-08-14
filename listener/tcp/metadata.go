package tcp

import (
	md "github.com/dolfly/core/metadata"
	mdutil "github.com/dolfly/x/metadata/util"
)

type metadata struct {
	mptcp bool
}

func (l *tcpListener) parseMetadata(md md.Metadata) (err error) {
	l.md.mptcp = mdutil.GetBool(md, "mptcp")

	return
}
