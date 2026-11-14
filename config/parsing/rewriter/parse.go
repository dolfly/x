package rewriter

import (
	"crypto/tls"
	"strings"

	"github.com/dolfly/core/logger"
	"github.com/dolfly/core/rewriter"
	"github.com/dolfly/x/config"
	"github.com/dolfly/x/internal/plugin"
	xplugin "github.com/dolfly/x/plugin"
	rewriter_plugin "github.com/dolfly/x/rewriter/plugin"
)

// ParseRewriter converts a RewriterConfig into a rewriter.Rewriter.
// It currently supports plugin backends only (HTTP or gRPC).
// Returns nil when cfg is nil or no backend is configured.
func ParseRewriter(cfg *config.RewriterConfig) rewriter.Rewriter {
	if cfg == nil {
		return nil
	}

	if cfg.Plugin != nil {
		if err := xplugin.Spawn(cfg.Plugin.Command); err != nil {
			logger.Default().Errorf("spawn plugin %s: %v", cfg.Name, err)
		}
		var tlsCfg *tls.Config
		if cfg.Plugin.TLS != nil {
			tlsCfg = &tls.Config{
				ServerName:         cfg.Plugin.TLS.ServerName,
				InsecureSkipVerify: !cfg.Plugin.TLS.Secure,
			}
		}
		switch strings.ToLower(cfg.Plugin.Type) {
		case "http":
			return rewriter_plugin.NewHTTPPlugin(
				cfg.Name, cfg.Plugin.Addr,
				plugin.TokenOption(cfg.Plugin.Token),
				plugin.TLSConfigOption(tlsCfg),
				plugin.TimeoutOption(cfg.Plugin.Timeout),
			)
		default:
			return rewriter_plugin.NewGRPCPlugin(
				cfg.Name, cfg.Plugin.Addr,
				plugin.TokenOption(cfg.Plugin.Token),
				plugin.TLSConfigOption(tlsCfg),
				plugin.TimeoutOption(cfg.Plugin.Timeout),
			)
		}
	}

	return nil
}
