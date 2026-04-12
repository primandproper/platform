package circuitbreakingcfg

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/primandproper/platform/circuitbreaking"
	"github.com/primandproper/platform/circuitbreaking/noop"
	"github.com/primandproper/platform/errors"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/metrics"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	circuit "github.com/rubyist/circuitbreaker"
)

type Config struct {
	Name                   string  `env:"NAME"                     json:"name"`
	ErrorRate              float64 `env:"ERROR_RATE"               json:"circuitBreakerErrorPercentage"`
	MinimumSampleThreshold uint64  `env:"MINIMUM_SAMPLE_THRESHOLD" json:"circuitBreakerMinimumOccurrenceThreshold"`
}

func (cfg *Config) EnsureDefaults() {
	if cfg.Name == "" {
		cfg.Name = "UNKNOWN"
	}

	if cfg.ErrorRate == 0 {
		cfg.ErrorRate = 100
	}

	if cfg.MinimumSampleThreshold == 0 {
		cfg.MinimumSampleThreshold = 1_000_000
	}
}

func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Name, validation.Required),
		validation.Field(&cfg.ErrorRate, validation.Min(0.00), validation.Max(100.0)),
		validation.Field(&cfg.MinimumSampleThreshold),
	)
}

// EnsureCircuitBreaker ensures a valid CircuitBreaker is made available.
func EnsureCircuitBreaker(breaker circuitbreaking.CircuitBreaker) circuitbreaking.CircuitBreaker {
	if breaker == nil {
		slog.Info("NOOP CircuitBreaker implementation in use.")
		return noop.NewCircuitBreaker()
	}

	return breaker
}

type baseImplementation struct {
	circuitBreaker *circuit.Breaker
}

func (b *baseImplementation) Failed() {
	b.circuitBreaker.Fail()
}

func (b *baseImplementation) Succeeded() {
	b.circuitBreaker.Success()
}

func (b *baseImplementation) CanProceed() bool {
	return b.circuitBreaker.Ready()
}

func (b *baseImplementation) CannotProceed() bool {
	return !b.circuitBreaker.Ready()
}

// ProvideCircuitBreaker provides a CircuitBreaker.
func (cfg *Config) ProvideCircuitBreaker(ctx context.Context, logger logging.Logger, metricsProvider metrics.Provider) (circuitbreaking.CircuitBreaker, error) {
	if cfg == nil {
		return nil, errors.ErrNilInputParameter
	}

	logger = logging.EnsureLogger(logger).WithValue("circuit_breaker", cfg.Name)

	if err := cfg.ValidateWithContext(ctx); err != nil {
		logger.Error("invalid config passed, providing noop circuit breaker", err)
		return noop.NewCircuitBreaker(), nil
	}

	cfg.EnsureDefaults()

	brokenCounter, err := metricsProvider.NewInt64Counter(fmt.Sprintf("%s_circuit_breaker_tripped", cfg.Name))
	if err != nil {
		return nil, err
	}

	failureCounter, err := metricsProvider.NewInt64Counter(fmt.Sprintf("%s_circuit_breaker_failed", cfg.Name))
	if err != nil {
		return nil, err
	}

	resetCounter, err := metricsProvider.NewInt64Counter(fmt.Sprintf("%s_circuit_breaker_reset", cfg.Name))
	if err != nil {
		return nil, err
	}

	cb := circuit.NewBreakerWithOptions(&circuit.Options{
		ShouldTrip: func(cb *circuit.Breaker) bool {
			return uint64(cb.Failures()+cb.Successes()) >= cfg.MinimumSampleThreshold && cb.ErrorRate() >= cfg.ErrorRate
		},
		WindowTime:    circuit.DefaultWindowTime,
		WindowBuckets: circuit.DefaultWindowBuckets,
	})

	events := cb.Subscribe()

	go handleCircuitBreakerEvents(ctx, logger, events, failureCounter, resetCounter, brokenCounter)

	return &baseImplementation{
		circuitBreaker: cb,
	}, nil
}

// ProvideCircuitBreakerFromConfig provides a CircuitBreaker from config.
func ProvideCircuitBreakerFromConfig(ctx context.Context, cfg *Config, logger logging.Logger, metricsProvider metrics.Provider) (circuitbreaking.CircuitBreaker, error) {
	return cfg.ProvideCircuitBreaker(ctx, logger, metricsProvider)
}

func handleCircuitBreakerEvents(
	ctx context.Context,
	logger logging.Logger,
	events <-chan circuit.BreakerEvent,
	failureCounter,
	resetCounter,
	brokenCounter metrics.Int64Counter,
) {
	for be := range events {
		switch be {
		case circuit.BreakerTripped:
			brokenCounter.Add(ctx, 1)
		case circuit.BreakerReset:
			resetCounter.Add(ctx, 1)
		case circuit.BreakerFail:
			failureCounter.Add(ctx, 1)
		case circuit.BreakerReady:
			logger.Debug("circuit breaker is ready")
		}
	}
}
