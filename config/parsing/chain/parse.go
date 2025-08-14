package chain

import (
	"github.com/dolfly/core/chain"
	"github.com/dolfly/core/hop"
	"github.com/dolfly/core/logger"
	"github.com/dolfly/core/metadata"
	xchain "github.com/dolfly/x/chain"
	"github.com/dolfly/x/config"
	hop_parser "github.com/dolfly/x/config/parsing/hop"
	mdx "github.com/dolfly/x/metadata"
	"github.com/dolfly/x/registry"
)

func ParseChain(cfg *config.ChainConfig, log logger.Logger) (chain.Chainer, error) {
	if cfg == nil {
		return nil, nil
	}

	chainLogger := log.WithFields(map[string]any{
		"kind":  "chain",
		"chain": cfg.Name,
	})

	var md metadata.Metadata
	if cfg.Metadata != nil {
		md = mdx.NewMetadata(cfg.Metadata)
	}

	c := xchain.NewChain(cfg.Name,
		xchain.MetadataChainOption(md),
		xchain.LoggerChainOption(chainLogger),
	)

	for _, ch := range cfg.Hops {
		var hop hop.Hop
		var err error

		if ch.Nodes != nil || ch.Plugin != nil {
			if hop, err = hop_parser.ParseHop(ch, log); err != nil {
				return nil, err
			}
		} else {
			hop = registry.HopRegistry().Get(ch.Name)
		}
		if hop != nil {
			c.AddHop(hop)
		}
	}

	return c, nil
}
