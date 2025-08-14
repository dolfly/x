package router

import (
	"time"

	"github.com/dolfly/core/ingress"
	mdata "github.com/dolfly/core/metadata"
	"github.com/dolfly/core/router"
	"github.com/dolfly/core/sd"
	mdutil "github.com/dolfly/x/metadata/util"
	"github.com/dolfly/x/registry"
)

const (
	defaultTTL             = 15 * time.Second
	defaultBufferSize      = 4096
	defaultCacheExpiration = time.Second
)

type metadata struct {
	readTimeout time.Duration
	bufferSize  int

	entryPoint        string
	ingress           ingress.Ingress
	sd                sd.SD
	sdCacheExpiration time.Duration
	sdRenewInterval   time.Duration

	router                router.Router
	routerCacheEnabled    bool
	routerCacheExpiration time.Duration

	observerPeriod       time.Duration
	observerResetTraffic bool

	limiterRefreshInterval time.Duration
	limiterCleanupInterval time.Duration
}

func (h *routerHandler) parseMetadata(md mdata.Metadata) (err error) {
	h.md.readTimeout = mdutil.GetDuration(md, "readTimeout")
	h.md.bufferSize = mdutil.GetInt(md, "router.bufferSize", "bufferSize")
	if h.md.bufferSize <= 0 {
		h.md.bufferSize = defaultBufferSize
	}

	h.md.entryPoint = mdutil.GetString(md, "entrypoint")
	h.md.ingress = registry.IngressRegistry().Get(mdutil.GetString(md, "ingress"))

	h.md.sd = registry.SDRegistry().Get(mdutil.GetString(md, "sd"))
	h.md.sdCacheExpiration = mdutil.GetDuration(md, "sd.cache.expiration")
	if h.md.sdCacheExpiration <= 0 {
		h.md.sdCacheExpiration = defaultCacheExpiration
	}
	h.md.sdRenewInterval = mdutil.GetDuration(md, "sd.renewInterval")
	if h.md.sdRenewInterval < time.Second {
		h.md.sdRenewInterval = defaultTTL
	}

	h.md.router = registry.RouterRegistry().Get(mdutil.GetString(md, "router"))
	h.md.routerCacheEnabled = mdutil.GetBool(md, "router.cache")
	h.md.routerCacheExpiration = mdutil.GetDuration(md, "router.cache.expiration")
	if h.md.routerCacheExpiration <= 0 {
		h.md.routerCacheExpiration = defaultCacheExpiration
	}

	h.md.observerPeriod = mdutil.GetDuration(md, "observePeriod", "observer.period", "observer.observePeriod")
	if h.md.observerPeriod == 0 {
		h.md.observerPeriod = 5 * time.Second
	}
	if h.md.observerPeriod < time.Second {
		h.md.observerPeriod = time.Second
	}
	h.md.observerResetTraffic = mdutil.GetBool(md, "observer.resetTraffic")

	h.md.limiterRefreshInterval = mdutil.GetDuration(md, "limiter.refreshInterval")
	h.md.limiterCleanupInterval = mdutil.GetDuration(md, "limiter.cleanupInterval")

	return
}
