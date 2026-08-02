package llmcfg

import (
	"context"
	"strings"

	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/llm"
	"github.com/primandproper/platform-go/v9/llm/anthropic"
	llmnoop "github.com/primandproper/platform-go/v9/llm/noop"
	"github.com/primandproper/platform-go/v9/llm/openai"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderOpenAI is the OpenAI provider.
	ProviderOpenAI = "openai"
	// ProviderAnthropic is the Anthropic provider.
	ProviderAnthropic = "anthropic"
	// ProviderNoop answers every request from a canned response and calls nothing.
	// It must be selected deliberately — an unset or typo'd provider is an error,
	// because an LLM that silently stops calling anything looks like a working
	// deployment whose answers have quietly become useless.
	ProviderNoop = "noop"
)

// Config is the configuration for the LLM provider.
type Config struct {
	OpenAI    *openai.Config    `env:",init"    envPrefix:"OPENAI_"    json:"openai"    yaml:"openai"`
	Anthropic *anthropic.Config `env:",init"    envPrefix:"ANTHROPIC_" json:"anthropic" yaml:"anthropic"`
	Provider  string            `env:"PROVIDER" json:"provider"        yaml:"provider"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates the config.
func (c *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, c,
		validation.Field(&c.Provider, validation.Required, validation.In(ProviderOpenAI, ProviderAnthropic, ProviderNoop)),
		validation.Field(&c.OpenAI, validation.When(c.Provider == ProviderOpenAI, validation.Required)),
		validation.Field(&c.Anthropic, validation.When(c.Provider == ProviderAnthropic, validation.Required)),
	)
}

// NewLLMProvider provides an LLM provider based on config.
func (c *Config) NewLLMProvider(_ context.Context, opts ...Option) (llm.Provider, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	switch strings.TrimSpace(strings.ToLower(c.Provider)) {
	case ProviderOpenAI:
		return openai.NewProvider(c.OpenAI, openai.WithLogger(logger), openai.WithTracerProvider(tracerProvider), openai.WithMetricsProvider(metricsProvider))
	case ProviderAnthropic:
		return anthropic.NewProvider(c.Anthropic, anthropic.WithLogger(logger), anthropic.WithTracerProvider(tracerProvider), anthropic.WithMetricsProvider(metricsProvider))
	case ProviderNoop:
		return llmnoop.NewProvider(), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "llm provider %q", c.Provider)
	}
}
