package vectorsearchcfg

import (
	"context"
	"strings"

	circuitbreakingcfg "github.com/primandproper/platform-go/v9/circuitbreaking/config"
	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"
	vectorsearch "github.com/primandproper/platform-go/v9/search/vector"
	"github.com/primandproper/platform-go/v9/search/vector/noop"
	"github.com/primandproper/platform-go/v9/search/vector/pgvector"
	"github.com/primandproper/platform-go/v9/search/vector/qdrant"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// PGvectorProvider selects the pgvector-backed vectorsearch.Index implementation.
	PGvectorProvider = "pgvector"
	// QdrantProvider selects the Qdrant-backed vectorsearch.Index implementation.
	QdrantProvider = "qdrant"
)

// Config dispatches to a vectorsearch provider implementation.
type Config struct {
	_              struct{}                  `json:"-"       yaml:"-"`
	Pgvector       *pgvector.Config          `env:",init"     envPrefix:"PGVECTOR_"        json:"pgvector"             yaml:"pgvector"`
	Qdrant         *qdrant.Config            `env:",init"     envPrefix:"QDRANT_"          json:"qdrant"               yaml:"qdrant"`
	Provider       string                    `env:"PROVIDER" json:"provider"              yaml:"provider"`
	CircuitBreaker circuitbreakingcfg.Config `env:",init"     envPrefix:"CIRCUIT_BREAKER_" json:"circuitBreakerConfig" yaml:"circuitBreakerConfig"`
}

// ProviderNoop indexes and searches nothing. It must be selected deliberately —
// an unset or typo'd provider is an error, because an index that quietly accepts
// every write and returns no hits looks like an empty corpus.
const ProviderNoop = "noop"

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct. Provider is canonicalized (trim + lowercase)
// first so validation matches the same normalization NewIndex dispatches on.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	cfg.Provider = strings.TrimSpace(strings.ToLower(cfg.Provider))

	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.Required, validation.In(PGvectorProvider, QdrantProvider, ProviderNoop)),
		validation.Field(&cfg.Pgvector, validation.When(cfg.Provider == PGvectorProvider, validation.Required)),
		validation.Field(&cfg.Qdrant, validation.When(cfg.Provider == QdrantProvider, validation.Required)),
	)
}

// NewIndex builds a vectorsearch.Index for the configured provider. The db
// argument is required only when Provider is PGvectorProvider; pass nil otherwise.
// An unknown or empty provider is an error; the noop index is reachable by
// naming it, matching the search/text dispatch convention.
func NewIndex[T any](
	ctx context.Context,
	logger logging.Logger,
	tracerProvider tracing.TracerProvider,
	metricsProvider metrics.Provider,
	cfg *Config,
	db database.Client,
	indexName string,
) (vectorsearch.Index[T], error) {
	if cfg == nil {
		return nil, vectorsearch.ErrNilConfig
	}

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating vector search config")
	}

	circuitBreaker, err := circuitbreakingcfg.NewCircuitBreaker(ctx, &cfg.CircuitBreaker, logger, metricsProvider)
	if err != nil {
		return nil, errors.Wrap(err, "initializing vector search circuit breaker")
	}

	switch strings.TrimSpace(strings.ToLower(cfg.Provider)) {
	case PGvectorProvider:
		return pgvector.NewIndex[T](ctx, cfg.Pgvector, db, indexName, circuitBreaker, pgvector.WithLogger(logger), pgvector.WithTracerProvider(tracerProvider), pgvector.WithMetricsProvider(metricsProvider))
	case QdrantProvider:
		return qdrant.NewIndex[T](ctx, cfg.Qdrant, indexName, circuitBreaker, qdrant.WithLogger(logger), qdrant.WithTracerProvider(tracerProvider), qdrant.WithMetricsProvider(metricsProvider))
	case ProviderNoop:
		return noop.NewIndex[T](), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "vector search provider %q", cfg.Provider)
	}
}
