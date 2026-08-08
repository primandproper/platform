package embeddingscfg

import (
	"context"

	"github.com/primandproper/platform-go/v10/embeddings"
	"github.com/primandproper/platform-go/v10/embeddings/cohere"
	"github.com/primandproper/platform-go/v10/embeddings/ollama"
	"github.com/primandproper/platform-go/v10/embeddings/openai"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderOpenAI is the OpenAI provider.
	ProviderOpenAI = "openai"
	// ProviderOllama is the Ollama provider.
	ProviderOllama = "ollama"
	// ProviderCohere is the Cohere provider.
	ProviderCohere = "cohere"
	// ProviderNoop names the embedder that returns no vectors. Embeddings are an
	// optional capability, so opting out is supported — but it has to be named,
	// or spelled as the empty provider.
	ProviderNoop = "noop"
)

// Config is the configuration for the embeddings provider.
type Config struct {
	OpenAI   *openai.Config `env:",init"    envPrefix:"OPENAI_"       json:"openai,omitempty"   yaml:"openai,omitempty"`
	Ollama   *ollama.Config `env:",init"    envPrefix:"OLLAMA_"       json:"ollama,omitempty"   yaml:"ollama,omitempty"`
	Cohere   *cohere.Config `env:",init"    envPrefix:"COHERE_"       json:"cohere,omitempty"   yaml:"cohere,omitempty"`
	Provider string         `env:"PROVIDER" json:"provider,omitempty" yaml:"provider,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates the config.
//
// The sub-configs for providers that were not selected are skipped rather than
// merely unguarded: ozzo validates any non-nil pointer to a Validatable once a
// field's rules have run, and `env:",init"` leaves every sub-config non-nil. A
// validation.When guard alone stops the Required rule and nothing else, so
// OpenAI's and Cohere's API keys were required at once and no config could load.
func (c *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, c,
		validation.Field(&c.Provider, validation.In(ProviderOpenAI, ProviderOllama, ProviderCohere, ProviderNoop, "")),
		validation.Field(&c.OpenAI, validation.Skip.When(c.Provider != ProviderOpenAI), validation.Required),
		validation.Field(&c.Ollama, validation.Skip.When(c.Provider != ProviderOllama), validation.Required),
		validation.Field(&c.Cohere, validation.Skip.When(c.Provider != ProviderCohere), validation.Required),
	)
}

// NewEmbedder provides an Embedder based on config.
func (c *Config) NewEmbedder(
	ctx context.Context,
	opts ...Option,
) (embeddings.Embedder, error) {
	return NewEmbedder(ctx, c, opts...)
}
