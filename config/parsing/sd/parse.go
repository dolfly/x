package sd

import (
	"crypto/tls"
	"strings"

	"github.com/dolfly/core/logger"
	"github.com/dolfly/core/sd"
	"github.com/dolfly/x/config"
	"github.com/dolfly/x/internal/plugin"
	xplugin "github.com/dolfly/x/plugin"
	sd_plugin "github.com/dolfly/x/sd/plugin"
)

// ParseSD converts an SDConfig into an sd.SD. It only supports plugin backends
// (HTTP or gRPC); returns nil when cfg or cfg.Plugin is nil.
func ParseSD(cfg *config.SDConfig) sd.SD {
	if cfg == nil || cfg.Plugin == nil {
		return nil
	}

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
		return sd_plugin.NewHTTPPlugin(
			cfg.Name, cfg.Plugin.Addr,
			plugin.TokenOption(cfg.Plugin.Token),
			plugin.TLSConfigOption(tlsCfg),
			plugin.TimeoutOption(cfg.Plugin.Timeout),
		)
	default:
		return sd_plugin.NewGRPCPlugin(
			cfg.Name, cfg.Plugin.Addr,
			plugin.TokenOption(cfg.Plugin.Token),
			plugin.TLSConfigOption(tlsCfg),
		)
	}
}
