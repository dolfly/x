package stdio

import (
	md "github.com/dolfly/core/metadata"
)

type metadata struct {
	// reserved for future options (e.g., timeouts)
}

func (l *stdioListener) parseMetadata(md md.Metadata) (err error) {
	return
}
