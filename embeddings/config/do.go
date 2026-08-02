package embeddingscfg

import (
	"context"

	"github.com/primandproper/platform-go/v9/embeddings"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	"github.com/samber/do/v2"
)

// RegisterEmbedder registers an embeddings.Embedder with the injector.
func RegisterEmbedder(i do.Injector) {
	do.Provide[embeddings.Embedder](i, func(i do.Injector) (embeddings.Embedder, error) {
		ctx := do.MustInvoke[context.Context](i)
		cfg := do.MustInvoke[*Config](i)
		logger := do.MustInvoke[logging.Logger](i)
		tracerProvider := do.MustInvoke[tracing.TracerProvider](i)
		metricsProvider := do.MustInvoke[metrics.Provider](i)
		return NewEmbedder(ctx, cfg, logger, tracerProvider, metricsProvider)
	})
}
