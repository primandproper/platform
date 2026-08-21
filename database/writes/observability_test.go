package writes_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/primandproper/platform-go/v12/database"
	"github.com/primandproper/platform-go/v12/database/writes"
	"github.com/primandproper/platform-go/v12/observability/metrics"
	metricsmock "github.com/primandproper/platform-go/v12/observability/metrics/mock"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// stageKey is the attribute a failure is labeled with. It is spelled out here
// rather than exported from the package, because what these tests assert is
// what a dashboard would find.
const stageKey = "database_writes.stage"

// counting is a metrics provider that records how many times each instrument
// was added to, and under which stage.
type counting struct {
	counts map[string]int64
	mu     sync.Mutex
}

func newCounting() *counting {
	return &counting{counts: map[string]int64{}}
}

func (c *counting) add(name string, delta int64, attrs attribute.Set) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.counts[name] += delta

	if stage, ok := attrs.Value(attribute.Key(stageKey)); ok {
		c.counts[name+"["+stage.AsString()+"]"] += delta
	}
}

func (c *counting) count(name string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.counts[name]
}

func (c *counting) provider() metrics.Provider {
	noop := metrics.EnsureMetricsProvider(nil)

	return &metricsmock.ProviderMock{
		NewInt64CounterFunc: func(name string, _ ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
			return &metricsmock.Int64CounterMock{
				AddFunc: func(_ context.Context, incr int64, opts ...metric.AddOption) {
					c.add(name, incr, metric.NewAddConfig(opts).Attributes())
				},
			}, nil
		},
		NewFloat64HistogramFunc: func(name string, _ ...metric.Float64HistogramOption) (metrics.Float64Histogram, error) {
			return &countingHistogram{name: name, into: c}, nil
		},
		NewInt64GaugeFunc: noop.NewInt64Gauge,
	}
}

// countingHistogram counts recordings rather than keeping them: what these
// tests assert is that a call was measured, and the duration it measured is the
// clock's business.
type countingHistogram struct {
	into *counting
	name string
}

var _ metrics.Float64Histogram = (*countingHistogram)(nil)

func (h *countingHistogram) Record(_ context.Context, _ float64, _ ...metric.RecordOption) {
	h.into.add(h.name, 1, *attribute.EmptySet())
}

func TestWriter_metrics(T *testing.T) {
	T.Parallel()

	T.Run("counts an attempt for every call and a failure for the ones that failed", func(t *testing.T) {
		t.Parallel()

		recorded := newCounting()
		_, writer, _ := newWriter(t, writes.WithMetricsProvider(recorded.provider()))

		must.NoError(t, writer.Do(t.Context(), func(context.Context, database.SQLQueryExecutor) ([]writes.Change, error) {
			return []writes.Change{newChange("widget_1", writes.OpCreated)}, nil
		}))

		test.Error(t, writer.Do(t.Context(), func(context.Context, database.SQLQueryExecutor) ([]writes.Change, error) {
			return nil, errors.New("statement failed")
		}))

		test.EqOp(t, int64(2), recorded.count("database_writes_requests"))
		test.EqOp(t, int64(1), recorded.count("database_writes_errors"))
		test.EqOp(t, int64(2), recorded.count("database_writes_latency_ms"))
	})

	T.Run("labels a failure with the stage that produced it", func(t *testing.T) {
		t.Parallel()

		recorded := newCounting()

		client := newClient(t)

		writer, err := writes.New(client,
			writes.WithMetricsProvider(recorded.provider()),
			writes.WithHook(func(context.Context, database.SQLQueryExecutor, *writes.Change) error {
				return errors.New("hook failed")
			}))
		must.NoError(t, err)

		// The domain's own statement failing and the audit hook failing are
		// different alerts, and arrive at the same return.
		test.Error(t, writer.Do(t.Context(), func(context.Context, database.SQLQueryExecutor) ([]writes.Change, error) {
			return nil, errors.New("statement failed")
		}))

		test.Error(t, writer.Do(t.Context(), func(context.Context, database.SQLQueryExecutor) ([]writes.Change, error) {
			return []writes.Change{newChange("widget_1", writes.OpCreated)}, nil
		}))

		test.Error(t, writer.Do(t.Context(), func(context.Context, database.SQLQueryExecutor) ([]writes.Change, error) {
			return []writes.Change{{Resource: "widget", Op: writes.OpCreated}}, nil
		}))

		test.EqOp(t, int64(3), recorded.count("database_writes_errors"))
		test.EqOp(t, int64(1), recorded.count("database_writes_errors[write]"))
		test.EqOp(t, int64(1), recorded.count("database_writes_errors[hook]"))
		test.EqOp(t, int64(1), recorded.count("database_writes_errors[change]"))
	})

	T.Run("a writer given no metrics provider records nowhere", func(t *testing.T) {
		t.Parallel()

		_, writer, _ := newWriter(t)

		must.NoError(t, writer.Do(t.Context(), func(context.Context, database.SQLQueryExecutor) ([]writes.Change, error) {
			return nil, nil
		}))
	})
}
