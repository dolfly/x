//go:build !linux

package router

import (
	"github.com/dolfly/core/router"
)

func (*localRouter) setSysRoutes(routes ...*router.Route) error {
	return nil
}
