package registry

import (
	"github.com/dolfly/core/service"
)

type serviceRegistry struct {
	registry[service.Service]
}
