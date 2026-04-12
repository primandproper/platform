package llmcfg

import (
	"context"

	"github.com/primandproper/platform/llm"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/metrics"
	"github.com/primandproper/platform/observability/tracing"
)

// ProvideLLMProvider provides an LLM provider from config.
func ProvideLLMProvider(c *Config, logger logging.Logger, tracerProvider tracing.TracerProvider, metricsProvider metrics.Provider) (llm.Provider, error) {
	return c.ProvideLLMProvider(context.Background(), logger, tracerProvider, metricsProvider)
}
