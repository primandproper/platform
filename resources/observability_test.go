package resources_test

import (
	"context"
	"sync"
	"testing"

	"github.com/primandproper/platform-go/v12/observability/metrics"
	metricsmock "github.com/primandproper/platform-go/v12/observability/metrics/mock"
	"github.com/primandproper/platform-go/v12/resources"
	"github.com/primandproper/platform-go/v12/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/metric"
)

// counting is a metrics provider that records how many times each instrument
// was added to.
//
// The trio's names are the component's, so a test asserting on them is asserting
// what a dashboard would find — see metrics.OperationSet.
type counting struct {
	counts map[string]int64
	mu     sync.Mutex
}

func newCounting() *counting {
	return &counting{counts: map[string]int64{}}
}

func (c *counting) add(name string, delta int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.counts[name] += delta
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
				AddFunc: func(_ context.Context, incr int64, _ ...metric.AddOption) { c.add(name, incr) },
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
	h.into.add(h.name, 1)
}

func TestStore_Metrics(T *testing.T) {
	T.Parallel()

	T.Run("counts an attempt for every call and a failure for the ones that failed", func(t *testing.T) {
		t.Parallel()

		recorded := newCounting()
		store, _ := newCommentStore(t, resources.WithMetricsProvider(recorded.provider()))
		ctx := t.Context()

		created, err := store.Create(ctx, tenancy.Global(), alice(), comment("counted", "recipe_metrics", "user_alice"))
		must.NoError(t, err)

		// A read that succeeds and a write that does not: bob is not the owner.
		_, err = store.Get(ctx, tenancy.Global(), bob(), created.ID)
		must.NoError(t, err)

		created.Content = "not bob's to edit"
		_, err = store.Update(ctx, tenancy.Global(), bob(), created)
		test.ErrorIs(t, err, resources.ErrNoRowsAffected)

		test.EqOp(t, int64(3), recorded.count("resources_requests"))
		test.EqOp(t, int64(1), recorded.count("resources_errors"))
		test.EqOp(t, int64(3), recorded.count("resources_latency_ms"))
	})

	T.Run("counts a failure the call rejected before it reached the database", func(t *testing.T) {
		t.Parallel()

		recorded := newCounting()
		store, _ := newCommentStore(t, resources.WithMetricsProvider(recorded.provider()))

		// The error rate a caller cares about includes the calls that never got
		// as far as a statement. Recording failures at the return rather than at
		// each execute is what puts them there.
		_, err := store.Get(t.Context(), tenancy.Global(), resources.Actor{}, "comment_1")
		test.ErrorIs(t, err, resources.ErrNoActor)

		test.EqOp(t, int64(1), recorded.count("resources_requests"))
		test.EqOp(t, int64(1), recorded.count("resources_errors"))
	})

	T.Run("a store given no metrics provider records nowhere", func(t *testing.T) {
		t.Parallel()

		store, _ := newCommentStore(t)

		_, err := store.Create(t.Context(), tenancy.Global(), alice(), comment("unmetered", "recipe_unmetered", "user_alice"))
		must.NoError(t, err)
	})
}
