package mqmetrics

import (
	"context"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/observability/keys"
	"github.com/primandproper/platform-go/v13/observability/metrics"
	metricsmock "github.com/primandproper/platform-go/v13/observability/metrics/mock"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// recordingCounter and recordingHistogram capture what was recorded through
// them, so a test can ask which of a broker's instruments a call reached rather
// than only that it did not panic.
type recordingCounter struct {
	adds []recorded
}

type recordingHistogram struct {
	records []recorded
}

type recorded struct {
	attrs attribute.Set
	value float64
}

// attrsOf resolves the measurement options an instrument was called with into
// the attribute set the exporter would see.
func attrsOf(options []metric.AddOption) attribute.Set {
	cfg := metric.NewAddConfig(options)

	return cfg.Attributes()
}

func (c *recordingCounter) Add(_ context.Context, incr int64, options ...metric.AddOption) {
	c.adds = append(c.adds, recorded{value: float64(incr), attrs: attrsOf(options)})
}

func (h *recordingHistogram) Record(_ context.Context, incr float64, options ...metric.RecordOption) {
	cfg := metric.NewRecordConfig(options)
	h.records = append(h.records, recorded{value: incr, attrs: cfg.Attributes()})
}

// instruments is a metrics.Provider handing out a distinct recorder per
// instrument name, keyed by that name, so a test can name the instrument it
// expects a call to have landed on.
type instruments struct {
	counters   map[string]*recordingCounter
	histograms map[string]*recordingHistogram
	*metricsmock.ProviderMock
}

func newInstruments() *instruments {
	i := &instruments{
		counters:   map[string]*recordingCounter{},
		histograms: map[string]*recordingHistogram{},
	}

	i.ProviderMock = &metricsmock.ProviderMock{
		NewInt64CounterFunc: func(name string, _ ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
			c := &recordingCounter{}
			i.counters[name] = c

			return c, nil
		},
		NewFloat64HistogramFunc: func(name string, _ ...metric.Float64HistogramOption) (metrics.Float64Histogram, error) {
			h := &recordingHistogram{}
			i.histograms[name] = h

			return h, nil
		},
	}

	return i
}

// topicOf reads the topic attribute off a recorded measurement, reporting
// whether one was attached at all.
func topicOf(t *testing.T, r recorded) (string, bool) {
	t.Helper()

	v, ok := r.attrs.Value(attribute.Key(keys.TopicKey))

	return v.AsString(), ok
}

// failingProvider builds instruments until the one named failOn is asked for,
// which is how each wrapped construction error is reached in turn.
func failingProvider(failOn string, cause error) *metricsmock.ProviderMock {
	return &metricsmock.ProviderMock{
		NewInt64CounterFunc: func(name string, _ ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
			if name == failOn {
				return nil, cause
			}

			return &recordingCounter{}, nil
		},
		NewFloat64HistogramFunc: func(name string, _ ...metric.Float64HistogramOption) (metrics.Float64Histogram, error) {
			if name == failOn {
				return nil, cause
			}

			return &recordingHistogram{}, nil
		},
	}
}

func TestNewPublisher(T *testing.T) {
	T.Parallel()

	T.Run("asks for the shared instrument names, not names built from the topic", func(t *testing.T) {
		t.Parallel()

		// The names are constants precisely so a dashboard has one series to
		// aggregate over, and so that a topic a broker accepts cannot fail
		// instrument construction by being an illegal instrument name. A topic
		// spelled as a PubSub subscription path exercises both at once.
		i := newInstruments()

		p, err := NewPublisher(i, "projects/p/subscriptions/s")
		must.NoError(t, err)
		must.NotNil(t, p)

		var asked []string
		for _, call := range i.NewInt64CounterCalls() {
			asked = append(asked, call.Name)
		}

		for _, call := range i.NewFloat64HistogramCalls() {
			asked = append(asked, call.Name)
		}

		test.Eq(t, []string{MessagesPublished, PublishErrors, PublishLatencyMS}, asked)
	})

	T.Run("a nil provider is resolved rather than dereferenced", func(t *testing.T) {
		t.Parallel()

		p, err := NewPublisher(nil, "topic")
		test.NoError(t, err)
		must.NotNil(t, p)

		// The noop instruments the nil resolves to still have to be callable.
		p.Failed(t.Context())
		p.Published(t.Context(), time.Now())
	})

	for _, instrument := range []string{MessagesPublished, PublishErrors, PublishLatencyMS} {
		T.Run("reports a failure to build "+instrument, func(t *testing.T) {
			t.Parallel()

			cause := errors.New("no meter")

			p, err := NewPublisher(failingProvider(instrument, cause), "topic")
			test.Nil(t, p)
			test.ErrorIs(t, err, cause)
		})
	}
}

func TestPublisher_Failed(T *testing.T) {
	T.Parallel()

	T.Run("counts the error and nothing else", func(t *testing.T) {
		t.Parallel()

		i := newInstruments()
		p, err := NewPublisher(i, "orders")
		must.NoError(t, err)

		p.Failed(t.Context())

		must.SliceLen(t, 1, i.counters[PublishErrors].adds)
		test.EqOp(t, float64(1), i.counters[PublishErrors].adds[0].value)

		// A publish that did not happen must not also count as one that did.
		test.SliceEmpty(t, i.counters[MessagesPublished].adds)
		test.SliceEmpty(t, i.histograms[PublishLatencyMS].records)
	})
}

func TestPublisher_Published(T *testing.T) {
	T.Parallel()

	T.Run("counts the publish and records its latency", func(t *testing.T) {
		t.Parallel()

		i := newInstruments()
		p, err := NewPublisher(i, "orders")
		must.NoError(t, err)

		p.Published(t.Context(), time.Now().Add(-25*time.Millisecond))

		must.SliceLen(t, 1, i.counters[MessagesPublished].adds)
		test.EqOp(t, float64(1), i.counters[MessagesPublished].adds[0].value)

		must.SliceLen(t, 1, i.histograms[PublishLatencyMS].records)
		test.GreaterEq(t, float64(25), i.histograms[PublishLatencyMS].records[0].value)

		test.SliceEmpty(t, i.counters[PublishErrors].adds)
	})

	T.Run("the topic rides along as an attribute", func(t *testing.T) {
		t.Parallel()

		// This is the other half of the constant instrument names: the topic has
		// to reach the exporter somehow, and it is an attribute on one instrument
		// rather than an instrument of its own.
		i := newInstruments()
		p, err := NewPublisher(i, "orders")
		must.NoError(t, err)

		p.Published(t.Context(), time.Now())
		p.Failed(t.Context())

		topic, ok := topicOf(t, i.counters[MessagesPublished].adds[0])
		must.True(t, ok)
		test.EqOp(t, "orders", topic)

		topic, ok = topicOf(t, i.histograms[PublishLatencyMS].records[0])
		must.True(t, ok)
		test.EqOp(t, "orders", topic)

		topic, ok = topicOf(t, i.counters[PublishErrors].adds[0])
		must.True(t, ok)
		test.EqOp(t, "orders", topic)
	})
}

func TestNewConsumer(T *testing.T) {
	T.Parallel()

	T.Run("asks for the shared instrument names", func(t *testing.T) {
		t.Parallel()

		i := newInstruments()

		c, err := NewConsumer(i, "https://sqs.us-east-1.amazonaws.com/1/q")
		must.NoError(t, err)
		must.NotNil(t, c)

		var asked []string
		for _, call := range i.NewInt64CounterCalls() {
			asked = append(asked, call.Name)
		}

		for _, call := range i.NewFloat64HistogramCalls() {
			asked = append(asked, call.Name)
		}

		test.Eq(t, []string{MessagesConsumed, HandlerErrors, ReceiveErrors, ConsumeLatencyMS}, asked)
	})

	T.Run("a nil provider is resolved rather than dereferenced", func(t *testing.T) {
		t.Parallel()

		c, err := NewConsumer(nil, "topic")
		test.NoError(t, err)
		must.NotNil(t, c)

		c.Handled(t.Context(), time.Now(), nil)
		c.ReceiveFailed(t.Context())
	})

	for _, instrument := range []string{MessagesConsumed, HandlerErrors, ReceiveErrors, ConsumeLatencyMS} {
		T.Run("reports a failure to build "+instrument, func(t *testing.T) {
			t.Parallel()

			cause := errors.New("no meter")

			c, err := NewConsumer(failingProvider(instrument, cause), "topic")
			test.Nil(t, c)
			test.ErrorIs(t, err, cause)
		})
	}
}

func TestConsumer_Handled(T *testing.T) {
	T.Parallel()

	T.Run("a successful handler counts as consumed", func(t *testing.T) {
		t.Parallel()

		i := newInstruments()
		c, err := NewConsumer(i, "orders")
		must.NoError(t, err)

		c.Handled(t.Context(), time.Now(), nil)

		must.SliceLen(t, 1, i.counters[MessagesConsumed].adds)
		test.SliceEmpty(t, i.counters[HandlerErrors].adds)
		test.SliceLen(t, 1, i.histograms[ConsumeLatencyMS].records)
	})

	T.Run("a failing handler counts as an error and not as consumed", func(t *testing.T) {
		t.Parallel()

		// The distinction this package exists to keep: MessagesConsumed counts
		// after the handler returns, so a consumer whose handler fails on every
		// message cannot show a healthy climbing consumed line.
		i := newInstruments()
		c, err := NewConsumer(i, "orders")
		must.NoError(t, err)

		c.Handled(t.Context(), time.Now(), errors.New("handler blew up"))

		test.SliceEmpty(t, i.counters[MessagesConsumed].adds)
		must.SliceLen(t, 1, i.counters[HandlerErrors].adds)
	})

	T.Run("latency is recorded either way", func(t *testing.T) {
		t.Parallel()

		// A handler that fails slowly is the case worth seeing, so the histogram
		// is written before the outcome is known.
		i := newInstruments()
		c, err := NewConsumer(i, "orders")
		must.NoError(t, err)

		c.Handled(t.Context(), time.Now().Add(-30*time.Millisecond), errors.New("slow failure"))

		must.SliceLen(t, 1, i.histograms[ConsumeLatencyMS].records)
		test.GreaterEq(t, float64(30), i.histograms[ConsumeLatencyMS].records[0].value)
	})
}

func TestConsumer_ReceiveFailed(T *testing.T) {
	T.Parallel()

	T.Run("counts against receive rather than handler errors", func(t *testing.T) {
		t.Parallel()

		// A broker that cannot be read from and a handler that was given a
		// message and failed are different outages, so they are different series.
		i := newInstruments()
		c, err := NewConsumer(i, "orders")
		must.NoError(t, err)

		c.ReceiveFailed(t.Context())

		must.SliceLen(t, 1, i.counters[ReceiveErrors].adds)
		test.SliceEmpty(t, i.counters[HandlerErrors].adds)
		test.SliceEmpty(t, i.counters[MessagesConsumed].adds)

		topic, ok := topicOf(t, i.counters[ReceiveErrors].adds[0])
		must.True(t, ok)
		test.EqOp(t, "orders", topic)
	})
}

func TestTopicAttr(T *testing.T) {
	T.Parallel()

	T.Run("carries the topic under the shared key", func(t *testing.T) {
		t.Parallel()

		cfg := metric.NewAddConfig([]metric.AddOption{topicAttr("orders")})
		attrs := cfg.Attributes()

		v, ok := attrs.Value(attribute.Key(keys.TopicKey))
		must.True(t, ok)
		test.EqOp(t, "orders", v.AsString())
	})
}
