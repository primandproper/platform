package healthcheck

import (
	"github.com/samber/do/v2"
)

// RegisterRegistry registers a Registry with the injector.
func RegisterRegistry(i do.Injector) {
	do.Provide[Registry](i, func(do.Injector) (Registry, error) {
		return NewRegistry(), nil
	})
}
