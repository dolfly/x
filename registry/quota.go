package registry

import "github.com/dolfly/x/limiter/quota"

// quotaLimiterRegistry holds shared quota limiters; Unregister closes them.
type quotaLimiterRegistry struct {
	registry[*quota.Limiter]
}
