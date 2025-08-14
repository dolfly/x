package registry

import (
	"github.com/dolfly/core/handler"
	"github.com/dolfly/core/logger"
)

type NewHandler func(opts ...handler.Option) handler.Handler

type handlerRegistry struct {
	registry[NewHandler]
}

func (r *handlerRegistry) Register(name string, v NewHandler) error {
	if err := r.registry.Register(name, v); err != nil {
		logger.Default().Fatal(err)
	}
	return nil
}
