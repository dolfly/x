package serial

import (
	"time"

	md "github.com/dolfly/core/metadata"
	mdutil "github.com/dolfly/core/metadata/util"
)

type metadata struct {
	timeout time.Duration
}

func (l *serialListener) parseMetadata(md md.Metadata) (err error) {
	l.md.timeout = mdutil.GetDuration(md, "timeout", "serial.timeout", "listener.serial.timeout")
	return
}
