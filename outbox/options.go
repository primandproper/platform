package outbox

import (
	"github.com/primandproper/platform-go/v9/clock"
	"github.com/primandproper/platform-go/v9/database/ddl"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"
)

// RelayOption configures a Relay.
type RelayOption func(*Relay)

// WithRelayClock swaps the clock driving the poll loop, leases, and backoff.
func WithRelayClock(c clock.Clock) RelayOption {
	return func(r *Relay) {
		if c != nil {
			r.clock = c
		}
	}
}

// WithRelayLogger attaches a logger. The relay reports every publish failure
// and every quarantine through it; without one, a queue that has stopped
// draining is visible only in metrics.
func WithRelayLogger(logger logging.Logger) RelayOption {
	return func(r *Relay) {
		r.logger = logger
	}
}

// WithRelayTracerProvider attaches a tracer provider. Cycles that claim nothing
// are not traced — a root span every poll interval is noise.
func WithRelayTracerProvider(tracerProvider tracing.TracerProvider) RelayOption {
	return func(r *Relay) {
		r.tracerProvider = tracerProvider
	}
}

// WithRelayMetricsProvider attaches a metrics provider.
func WithRelayMetricsProvider(metricsProvider metrics.Provider) RelayOption {
	return func(r *Relay) {
		r.metricsProvider = metricsProvider
	}
}

// WriterOption configures a Writer.
type WriterOption func(*Writer)

// WithWriterTablePrefix overrides DefaultTablePrefix. The namespace must be a
// plain SQL identifier fragment with no trailing separator: it is interpolated
// into the query text, not bound as a parameter, and it must match the one the
// migrations were rendered with.
func WithWriterTablePrefix(prefix string) WriterOption {
	return func(w *Writer) {
		if prefix != "" {
			w.table = ddl.Qualify(prefix) + "outbox_messages"
		}
	}
}

// WithWriterClock swaps the clock used to stamp created_at and next_attempt.
func WithWriterClock(c clock.Clock) WriterOption {
	return func(w *Writer) {
		if c != nil {
			w.clock = c
		}
	}
}

// WithWriterLogger attaches a logger.
func WithWriterLogger(logger logging.Logger) WriterOption {
	return func(w *Writer) {
		w.logger = logger
	}
}

// WithWriterTracerProvider attaches a tracer provider, so an Enqueue shows up
// as a child of the span that owns the transaction.
func WithWriterTracerProvider(tracerProvider tracing.TracerProvider) WriterOption {
	return func(w *Writer) {
		w.tracerProvider = tracerProvider
	}
}

// WithWriterMetricsProvider attaches a metrics provider, enabling
// outbox_messages_enqueued. Pair it with the Relay's provider: enqueue rate
// against publish rate is what tells you whether the relay is keeping up, and
// neither number answers that alone.
func WithWriterMetricsProvider(metricsProvider metrics.Provider) WriterOption {
	return func(w *Writer) {
		w.metricsProvider = metricsProvider
	}
}
