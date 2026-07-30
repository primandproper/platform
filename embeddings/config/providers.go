package embeddingscfg

import (
	"context"
	"strings"

	"github.com/primandproper/platform-go/v8/embeddings"
	"github.com/primandproper/platform-go/v8/embeddings/cohere"
	embeddingsnoop "github.com/primandproper/platform-go/v8/embeddings/noop"
	"github.com/primandproper/platform-go/v8/embeddings/ollama"
	"github.com/primandproper/platform-go/v8/embeddings/openai"
	"github.com/primandproper/platform-go/v8/observability/logging"
	"github.com/primandproper/platform-go/v8/observability/tracing"
)

// NewEmbedder provides an Embedder from config.
func NewEmbedder(ctx context.Context, c *Config, logger logging.Logger, tracerProvider tracing.TracerProvider) (embeddings.Embedder, error) {
	switch strings.TrimSpace(strings.ToLower(c.Provider)) {
	case ProviderOpenAI:
		return openai.NewEmbedder(ctx, c.OpenAI, openai.WithLogger(logger), openai.WithTracerProvider(tracerProvider))
	case ProviderOllama:
		return ollama.NewEmbedder(ctx, c.Ollama, ollama.WithLogger(logger), ollama.WithTracerProvider(tracerProvider))
	case ProviderCohere:
		return cohere.NewEmbedder(ctx, c.Cohere, cohere.WithLogger(logger), cohere.WithTracerProvider(tracerProvider))
	default:
		return embeddingsnoop.NewEmbedder(), nil
	}
}
