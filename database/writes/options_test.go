package writes_test

import (
	"context"
	"errors"
	"testing"

	"github.com/primandproper/platform-go/v12/database"
	"github.com/primandproper/platform-go/v12/database/writes"
	"github.com/primandproper/platform-go/v12/observability"
	loggingnoop "github.com/primandproper/platform-go/v12/observability/logging/noop"
	"github.com/primandproper/platform-go/v12/observability/metrics"
	metricsmock "github.com/primandproper/platform-go/v12/observability/metrics/mock"
	tracingnoop "github.com/primandproper/platform-go/v12/observability/tracing/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/metric"
)

func TestOptions(T *testing.T) {
	T.Parallel()

	T.Run("the pillars supply all three", func(t *testing.T) {
		t.Parallel()

		recorded := newCounting()

		writer, err := writes.New(newClient(t), writes.WithPillars(&observability.Pillars{
			Logger:          loggingnoop.NewLogger(),
			TracerProvider:  tracingnoop.NewTracerProvider(),
			MetricsProvider: recorded.provider(),
		}))
		must.NoError(t, err)

		must.NoError(t, writer.Do(t.Context(), func(context.Context, database.SQLQueryExecutor) ([]writes.Change, error) {
			return nil, nil
		}))

		test.EqOp(t, int64(1), recorded.count("database_writes_requests"))
	})

	T.Run("a later option overrides what the pillars set", func(t *testing.T) {
		t.Parallel()

		recorded := newCounting()

		// Options apply in order, so this one writer is unmetered while the rest
		// of the deployment is not.
		writer, err := writes.New(newClient(t),
			writes.WithPillars(&observability.Pillars{MetricsProvider: recorded.provider()}),
			writes.WithMetricsProvider(nil))
		must.NoError(t, err)

		must.NoError(t, writer.Do(t.Context(), func(context.Context, database.SQLQueryExecutor) ([]writes.Change, error) {
			return nil, nil
		}))

		test.EqOp(t, int64(0), recorded.count("database_writes_requests"))
	})

	T.Run("nil pillars leave every dependency to its noop", func(t *testing.T) {
		t.Parallel()

		writer, err := writes.New(newClient(t), writes.WithPillars(nil), writes.WithLogger(nil), writes.WithTracerProvider(nil))
		must.NoError(t, err)

		must.NoError(t, writer.Do(t.Context(), func(context.Context, database.SQLQueryExecutor) ([]writes.Change, error) {
			return nil, nil
		}))
	})

	T.Run("an instrument that cannot be built fails construction", func(t *testing.T) {
		t.Parallel()

		broken := errors.New("no meter")

		writer, err := writes.New(newClient(t), writes.WithMetricsProvider(&metricsmock.ProviderMock{
			NewInt64CounterFunc: func(string, ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
				return nil, broken
			},
		}))

		test.Nil(t, writer)
		test.ErrorIs(t, err, broken)
	})
}
