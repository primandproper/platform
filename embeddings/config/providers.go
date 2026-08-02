package embeddingscfg

import (
	"context"
	"strings"

	"github.com/primandproper/platform-go/v9/embeddings"
	"github.com/primandproper/platform-go/v9/embeddings/cohere"
	embeddingsnoop "github.com/primandproper/platform-go/v9/embeddings/noop"
	"github.com/primandproper/platform-go/v9/embeddings/ollama"
	"github.com/primandproper/platform-go/v9/embeddings/openai"
	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"
)

// NewEmbedder provides an Embedder from config.
//
// An unrecognized provider is an error rather than a noop embedder, so that a
// misspelling surfaces at construction instead of as silently absent vectors.
// The empty provider is the one exception: embeddings are an optional
// capability, so leaving them unconfigured is a supported deployment and yields
// the noop embedder, which ProviderNoop also names explicitly.
func NewEmbedder(
	ctx context.Context,
	c *Config,
	logger logging.Logger,
	tracerProvider tracing.TracerProvider,
	metricsProvider metrics.Provider,
) (embeddings.Embedder, error) {
	if c == nil {
		return nil, errors.ErrNilInputParameter
	}

	switch provider := strings.TrimSpace(strings.ToLower(c.Provider)); provider {
	case ProviderOpenAI:
		return openai.NewEmbedder(ctx, c.OpenAI,
			openai.WithLogger(logger),
			openai.WithTracerProvider(tracerProvider),
			openai.WithMetricsProvider(metricsProvider),
		)
	case ProviderOllama:
		return ollama.NewEmbedder(ctx, c.Ollama,
			ollama.WithLogger(logger),
			ollama.WithTracerProvider(tracerProvider),
			ollama.WithMetricsProvider(metricsProvider),
		)
	case ProviderCohere:
		return cohere.NewEmbedder(ctx, c.Cohere,
			cohere.WithLogger(logger),
			cohere.WithTracerProvider(tracerProvider),
			cohere.WithMetricsProvider(metricsProvider),
		)
	case ProviderNoop, "":
		return embeddingsnoop.NewEmbedder(), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "embeddings provider %q", c.Provider)
	}
}
