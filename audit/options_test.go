package audit

import (
	"testing"
	"time"

	"github.com/primandproper/platform-go/v9/clock"
	"github.com/primandproper/platform-go/v9/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	loggingnoop "github.com/primandproper/platform-go/v9/observability/logging/noop"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	metricsmock "github.com/primandproper/platform-go/v9/observability/metrics/mock"
	tracingnoop "github.com/primandproper/platform-go/v9/observability/tracing/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/metric"
)

// errInstrument is what the failing metrics provider reports.
var errInstrument = platformerrors.New("instrument unavailable")

// failingMetricsProvider fails to build the nth instrument it is asked for, and
// succeeds on every other.
//
// Each constructor here builds its instruments in a fixed order and wraps each
// failure with its own description, so walking n across the count is what proves
// no branch reports another's error — a misattributed wrap in a constructor is
// invisible until someone is reading a boot failure at three in the morning.
func failingMetricsProvider(failAt int) metrics.Provider {
	provider := metrics.EnsureMetricsProvider(nil)
	calls := 0

	guard := func() error {
		calls++
		if calls == failAt {
			return errInstrument
		}

		return nil
	}

	return &metricsmock.ProviderMock{
		NewInt64CounterFunc: func(name string, options ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
			if err := guard(); err != nil {
				return nil, err
			}

			return provider.NewInt64Counter(name, options...)
		},
		NewFloat64HistogramFunc: func(name string, options ...metric.Float64HistogramOption) (metrics.Float64Histogram, error) {
			if err := guard(); err != nil {
				return nil, err
			}

			return provider.NewFloat64Histogram(name, options...)
		},
		NewInt64GaugeFunc: func(name string, options ...metric.Int64GaugeOption) (metrics.Int64Gauge, error) {
			if err := guard(); err != nil {
				return nil, err
			}

			return provider.NewInt64Gauge(name, options...)
		},
	}
}

func TestRecorderOptions(T *testing.T) {
	T.Parallel()

	T.Run("apply what they are given", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		logger := loggingnoop.NewLogger()
		tracerProvider := tracingnoop.NewTracerProvider()
		metricsProvider := metrics.EnsureMetricsProvider(nil)

		r := &recorder{}
		for _, opt := range []RecorderOption{
			WithRecorderTablePrefix("custom"),
			WithRecorderClock(c),
			WithRecorderLogger(logger),
			WithRecorderTracerProvider(tracerProvider),
			WithRecorderMetricsProvider(metricsProvider),
			WithRedaction("user", Redaction{Drop: []string{"password"}}),
		} {
			opt(r)
		}

		test.EqOp(t, "custom", r.prefix)
		test.EqOp(t, clock.Clock(c), r.clock)
		test.EqOp(t, logger, r.logger)
		test.NotNil(t, r.tracerProvider)
		test.NotNil(t, r.metricsProvider)
		test.MapLen(t, 1, r.redactions)
	})

	T.Run("ignore empty and nil values rather than clobbering a default", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		r := &recorder{prefix: DefaultTablePrefix, clock: c}

		WithRecorderTablePrefix("")(r)
		WithRecorderClock(nil)(r)

		test.EqOp(t, DefaultTablePrefix, r.prefix)
		test.EqOp(t, clock.Clock(c), r.clock)
	})

	T.Run("reports an instrument that cannot be built", func(t *testing.T) {
		t.Parallel()

		for failAt := 1; failAt <= 2; failAt++ {
			_, err := NewRecorder(dialect.SQLite, WithRecorderMetricsProvider(failingMetricsProvider(failAt)))
			test.ErrorIs(t, err, errInstrument, test.Sprintf("instrument %d", failAt))
		}
	})
}

func TestReaderOptions(T *testing.T) {
	T.Parallel()

	T.Run("apply what they are given", func(t *testing.T) {
		t.Parallel()

		logger := loggingnoop.NewLogger()

		r := &reader{}
		for _, opt := range []ReaderOption{
			WithReaderTablePrefix("custom"),
			WithReaderLogger(logger),
			WithReaderTracerProvider(tracingnoop.NewTracerProvider()),
			WithReaderMetricsProvider(metrics.EnsureMetricsProvider(nil)),
		} {
			opt(r)
		}

		test.EqOp(t, "custom", r.prefix)
		test.EqOp(t, logger, r.logger)
		test.NotNil(t, r.tracerProvider)
		test.NotNil(t, r.metricsProvider)
	})

	T.Run("ignore an empty prefix", func(t *testing.T) {
		t.Parallel()

		r := &reader{prefix: DefaultTablePrefix}
		WithReaderTablePrefix("")(r)

		test.EqOp(t, DefaultTablePrefix, r.prefix)
	})

	T.Run("reports an instrument that cannot be built", func(t *testing.T) {
		t.Parallel()

		for failAt := 1; failAt <= 2; failAt++ {
			_, err := NewReader(newTestClient(t),
				WithReaderMetricsProvider(failingMetricsProvider(failAt)))
			test.ErrorIs(t, err, errInstrument, test.Sprintf("instrument %d", failAt))
		}
	})

	T.Run("ignores nil options", func(t *testing.T) {
		t.Parallel()

		r, err := NewReader(newTestClient(t), nil)
		must.NoError(t, err)
		test.NotNil(t, r)
	})
}

func TestSweeperOptions(T *testing.T) {
	T.Parallel()

	T.Run("apply what they are given", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		logger := loggingnoop.NewLogger()

		s := &Sweeper{}
		for _, opt := range []SweeperOption{
			WithSweeperClock(c),
			WithSweeperLogger(logger),
			WithSweeperTracerProvider(tracingnoop.NewTracerProvider()),
			WithSweeperMetricsProvider(metrics.EnsureMetricsProvider(nil)),
		} {
			opt(s)
		}

		test.EqOp(t, clock.Clock(c), s.clock)
		test.EqOp(t, logger, s.logger)
		test.NotNil(t, s.tracerProvider)
		test.NotNil(t, s.metricsProvider)
	})

	T.Run("ignore a nil clock", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		s := &Sweeper{clock: c}
		WithSweeperClock(nil)(s)

		test.EqOp(t, clock.Clock(c), s.clock)
	})

	T.Run("reports an instrument that cannot be built", func(t *testing.T) {
		t.Parallel()

		for failAt := 1; failAt <= 3; failAt++ {
			_, err := NewSweeper(t.Context(),
				&SweeperConfig{Dialect: dialect.SQLite, Retention: time.Hour},
				newTestClient(t),
				WithSweeperMetricsProvider(failingMetricsProvider(failAt)))
			test.ErrorIs(t, err, errInstrument, test.Sprintf("instrument %d", failAt))
		}
	})

	T.Run("ignores nil options", func(t *testing.T) {
		t.Parallel()

		s, err := NewSweeper(t.Context(),
			&SweeperConfig{Dialect: dialect.SQLite, Retention: time.Hour}, newTestClient(t), nil)
		must.NoError(t, err)
		test.NotNil(t, s)
	})

	T.Run("rejects an invalid config", func(t *testing.T) {
		t.Parallel()

		_, err := NewSweeper(t.Context(),
			&SweeperConfig{Dialect: "cassandra"}, newTestClient(t))
		test.Error(t, err)
	})
}
