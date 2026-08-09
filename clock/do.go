package clock

import (
	"github.com/samber/do/v2"
)

// RegisterClock registers the wall Clock with the injector.
func RegisterClock(i do.Injector) {
	do.Provide(i, func(i do.Injector) (Clock, error) {
		return NewClock(), nil
	})
}
