package jobs_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v8/messagequeue"
	mockmq "github.com/primandproper/platform-go/v8/messagequeue/mock"
	"github.com/primandproper/platform-go/v8/observability/metrics"
	mockmetrics "github.com/primandproper/platform-go/v8/observability/metrics/mock"
	metricsnoop "github.com/primandproper/platform-go/v8/observability/metrics/noop"

	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/metric"
)

// testTopic is the topic every Pool fixture consumes.
const testTopic = "test-topic"

// waitFor is the ceiling on any "this should have happened by now" wait. The
// tests synchronize on channels rather than sleeping, so it is only ever
// reached when something is actually wrong.
const waitFor = 10 * time.Second

// fakeQueue is a ConsumerProvider whose consumer reads from a channel a test
// controls. It stands in for a broker: publish() puts a payload on the wire,
// and the Pool's own handler is whatever it registered at construction.
type fakeQueue struct {
	provider messagequeue.ConsumerProvider
	messages chan []byte
	stopped  chan struct{}

	handler messagequeue.ConsumerFunc
	errs    []error
	mu      sync.Mutex
}

// newFakeQueue builds a queue whose consumer runs until its stop channel
// closes, mirroring what every real implementation in messagequeue does.
func newFakeQueue() *fakeQueue {
	q := &fakeQueue{
		messages: make(chan []byte, 64),
		stopped:  make(chan struct{}),
	}

	consumer := &mockmq.ConsumerMock{
		ConsumeFunc: func(ctx context.Context, stopChan chan bool, errs chan error) {
			defer close(q.stopped)

			for {
				select {
				case <-stopChan:
					return
				case <-ctx.Done():
					return
				case msg := <-q.messages:
					if err := q.handlerFunc()(ctx, msg); err != nil {
						q.recordErr(err)

						select {
						case errs <- err:
						default:
						}
					}
				}
			}
		},
	}

	q.provider = &mockmq.ConsumerProviderMock{
		CloseFunc: func() {},
		NewConsumerFunc: func(_ context.Context, _ string, handlerFunc messagequeue.ConsumerFunc) (messagequeue.Consumer, error) {
			q.mu.Lock()
			defer q.mu.Unlock()
			q.handler = handlerFunc

			return consumer, nil
		},
	}

	return q
}

func (q *fakeQueue) handlerFunc() messagequeue.ConsumerFunc {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.handler
}

func (q *fakeQueue) recordErr(err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.errs = append(q.errs, err)
}

// handlerErrors returns the errors the Pool's handler returned to the consumer.
// In steady state this is empty: the Pool hands messages off and reports
// success immediately, so only a shutdown rejection shows up here.
func (q *fakeQueue) handlerErrors() []error {
	q.mu.Lock()
	defer q.mu.Unlock()

	return append([]error(nil), q.errs...)
}

func (q *fakeQueue) publish(payloads ...string) {
	for _, payload := range payloads {
		q.messages <- []byte(payload)
	}
}

// counterSpy is a metrics.Provider that totals Int64Counter increments by
// instrument name, so a test can assert on the counters that are the only
// externally visible record of a dropped or dead-lettered message.
type counterSpy struct {
	counts map[string]int64
	mu     sync.Mutex
}

func newCounterSpy() *counterSpy {
	return &counterSpy{counts: map[string]int64{}}
}

func (c *counterSpy) add(name string, incr int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[name] += incr
}

func (c *counterSpy) count(name string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.counts[name]
}

// provider delegates everything but Int64Counter to the noop implementation:
// only the counters carry assertions, and the histograms would otherwise need a
// double each.
func (c *counterSpy) provider() metrics.Provider {
	fallback := metricsnoop.NewMetricsProvider()

	return &mockmetrics.ProviderMock{
		NewFloat64HistogramFunc:   fallback.NewFloat64Histogram,
		NewInt64UpDownCounterFunc: fallback.NewInt64UpDownCounter,
		NewInt64CounterFunc: func(name string, _ ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
			return &mockmetrics.Int64CounterMock{
				AddFunc: func(context.Context, int64, ...metric.AddOption) { c.add(name, 1) },
			}, nil
		},
	}
}

// awaitCount blocks until the named counter reaches at least want. Polling is
// the only option — the counter is written from a worker goroutine with no
// channel to wait on — but the poll interval never matters, because the assert
// below it only runs once the value has arrived or waitFor has elapsed.
func awaitCount(t *testing.T, spy *counterSpy, name string, want int64) {
	t.Helper()

	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		if spy.count(name) >= want {
			return
		}

		time.Sleep(time.Millisecond)
	}

	must.Unreachable(t, must.Sprintf("counter %q reached %d, wanted %d", name, spy.count(name), want))
}

// recv takes one value from ch or fails, so a test that would otherwise hang
// reports which wait it was stuck on.
func recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(waitFor):
		must.Unreachable(t, must.Sprintf("timed out waiting for %s", what))

		var zero T

		return zero
	}
}

// notYet asserts nothing has arrived on ch. It is deliberately impatient: it is
// used to show that a Close has not returned yet, and a long wait there would
// only slow the test down without strengthening the claim.
func notYet[T any](t *testing.T, ch <-chan T, what string) {
	t.Helper()

	select {
	case <-ch:
		must.Unreachable(t, must.Sprintf("%s happened earlier than it should have", what))
	case <-time.After(50 * time.Millisecond):
	}
}
