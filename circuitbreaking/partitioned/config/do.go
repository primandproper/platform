package partitionedcfg

import (
	"context"

	"github.com/primandproper/platform-go/v8/circuitbreaking/partitioned"
	"github.com/primandproper/platform-go/v8/observability/logging"
	"github.com/primandproper/platform-go/v8/observability/metrics"

	"github.com/samber/do/v2"
)

// RegisterKeyedCircuitBreaker registers a KeyedCircuitBreaker with the injector.
func RegisterKeyedCircuitBreaker(i do.Injector) {
	do.Provide[partitioned.KeyedCircuitBreaker](i, func(i do.Injector) (partitioned.KeyedCircuitBreaker, error) {
		return NewKeyedCircuitBreaker(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[logging.Logger](i),
			do.MustInvoke[metrics.Provider](i),
		)
	})
}
