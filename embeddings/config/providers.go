package embeddingscfg

import (
	"context"

	"github.com/primandproper/platform-go/v10/embeddings"
	"github.com/primandproper/platform-go/v10/embeddings/cohere"
	embeddingsnoop "github.com/primandproper/platform-go/v10/embeddings/noop"
	"github.com/primandproper/platform-go/v10/embeddings/ollama"
	"github.com/primandproper/platform-go/v10/embeddings/openai"
	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/internal/cfgnorm"
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
	opts ...Option,
) (embeddings.Embedder, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	if c == nil {
		return nil, errors.ErrNilInputParameter
	}

	provider, err := cfgnorm.SelectProvider(c.Provider, providers, "embeddings provider")
	if err != nil {
		return nil, err
	}

	if err = c.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating embeddings config")
	}

	switch provider {
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
