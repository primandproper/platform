package embeddingscfg

import (
	"context"
	"strings"

	"github.com/primandproper/platform/embeddings"
	"github.com/primandproper/platform/embeddings/cohere"
	"github.com/primandproper/platform/embeddings/ollama"
	"github.com/primandproper/platform/embeddings/openai"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/tracing"
)

// ProvideEmbedder provides an Embedder from config.
func ProvideEmbedder(ctx context.Context, c *Config, logger logging.Logger, tracer tracing.Tracer) (embeddings.Embedder, error) {
	switch strings.TrimSpace(strings.ToLower(c.Provider)) {
	case ProviderOpenAI:
		return openai.NewEmbedder(ctx, c.OpenAI, logger, tracer)
	case ProviderOllama:
		return ollama.NewEmbedder(ctx, c.Ollama, logger, tracer)
	case ProviderCohere:
		return cohere.NewEmbedder(ctx, c.Cohere, logger, tracer)
	default:
		return embeddings.NewNoopEmbedder(), nil
	}
}
