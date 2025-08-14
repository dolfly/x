package http3

import (
	"net/http"
	"strings"

	mdata "github.com/dolfly/core/metadata"
	mdutil "github.com/dolfly/x/metadata/util"
)

type metadata struct {
	probeResistance *probeResistance
	header          http.Header
	hash            string
}

func (h *http3Handler) parseMetadata(md mdata.Metadata) error {
	if m := mdutil.GetStringMapString(md, "header"); len(m) > 0 {
		hd := http.Header{}
		for k, v := range m {
			hd.Add(k, v)
		}
		h.md.header = hd
	}

	pr := mdutil.GetString(md, "probeResistance", "probe_resist")
	if pr != "" {
		if ss := strings.SplitN(pr, ":", 2); len(ss) == 2 {
			h.md.probeResistance = &probeResistance{
				Type:  ss[0],
				Value: ss[1],
				Knock: mdutil.GetString(md, "knock"),
			}
		}
	}
	h.md.hash = mdutil.GetString(md, "hash")

	return nil
}

type probeResistance struct {
	Type  string
	Value string
	Knock string
}
