package metrics

import (
	"context"

	"go.opentelemetry.io/otel/metric"
)

type (
	Provider interface {
		NewFloat64Counter(name string, options ...metric.Float64CounterOption) (Float64Counter, error)
		NewFloat64Gauge(name string, options ...metric.Float64GaugeOption) (Float64Gauge, error)
		NewFloat64UpDownCounter(name string, options ...metric.Float64UpDownCounterOption) (Float64UpDownCounter, error)
		NewFloat64Histogram(name string, options ...metric.Float64HistogramOption) (Float64Histogram, error)
		NewInt64Counter(name string, options ...metric.Int64CounterOption) (Int64Counter, error)
		NewInt64Gauge(name string, options ...metric.Int64GaugeOption) (Int64Gauge, error)
		NewInt64UpDownCounter(name string, options ...metric.Int64UpDownCounterOption) (Int64UpDownCounter, error)
		NewInt64Histogram(name string, options ...metric.Int64HistogramOption) (Int64Histogram, error)
		Shutdown(ctx context.Context) error
		MeterProvider() metric.MeterProvider
	}

	Float64Counter interface {
		Add(ctx context.Context, incr float64, options ...metric.AddOption)
	}

	Float64Gauge interface {
		Record(ctx context.Context, value float64, options ...metric.RecordOption)
	}

	Float64UpDownCounter interface {
		Add(ctx context.Context, incr float64, options ...metric.AddOption)
	}

	Float64Histogram interface {
		Record(ctx context.Context, incr float64, options ...metric.RecordOption)
	}

	Int64Counter interface {
		Add(ctx context.Context, incr int64, options ...metric.AddOption)
	}

	Int64Gauge interface {
		Record(ctx context.Context, value int64, options ...metric.RecordOption)
	}

	Int64UpDownCounter interface {
		Add(ctx context.Context, incr int64, options ...metric.AddOption)
	}

	Int64Histogram interface {
		Record(ctx context.Context, incr int64, options ...metric.RecordOption)
	}

	Float64CounterImpl struct {
		X metric.Float64Counter
	}

	Float64GaugeImpl struct {
		X metric.Float64Gauge
	}

	Float64UpDownCounterImpl struct {
		X metric.Float64UpDownCounter
	}

	Float64HistogramImpl struct {
		X metric.Float64Histogram
	}

	Int64CounterImpl struct {
		X metric.Int64Counter
	}

	Int64GaugeImpl struct {
		X metric.Int64Gauge
	}

	Int64UpDownCounterImpl struct {
		X metric.Int64UpDownCounter
	}

	Int64HistogramImpl struct {
		X metric.Int64Histogram
	}
)

// Add wraps the metric float64Counter interface.
func (y *Float64CounterImpl) Add(ctx context.Context, incr float64, options ...metric.AddOption) {
	y.X.Add(ctx, incr, options...)
}

// Record wraps the metric float64Gauge interface.
func (y *Float64GaugeImpl) Record(ctx context.Context, value float64, options ...metric.RecordOption) {
	y.X.Record(ctx, value, options...)
}

// Add wraps the metric float64UpDownCounter interface.
func (y *Float64UpDownCounterImpl) Add(ctx context.Context, incr float64, options ...metric.AddOption) {
	y.X.Add(ctx, incr, options...)
}

// Record wraps the metric float64Histogram interface.
func (y *Float64HistogramImpl) Record(ctx context.Context, incr float64, options ...metric.RecordOption) {
	y.X.Record(ctx, incr, options...)
}

// Add wraps the metric int64Counter interface.
func (y *Int64CounterImpl) Add(ctx context.Context, incr int64, options ...metric.AddOption) {
	y.X.Add(ctx, incr, options...)
}

// Record wraps the metric int64Gauge interface.
func (y *Int64GaugeImpl) Record(ctx context.Context, value int64, options ...metric.RecordOption) {
	y.X.Record(ctx, value, options...)
}

// Add wraps the metric int64UpDownCounter interface.
func (y *Int64UpDownCounterImpl) Add(ctx context.Context, incr int64, options ...metric.AddOption) {
	y.X.Add(ctx, incr, options...)
}

// Record wraps the metric int64Histogram interface.
func (y *Int64HistogramImpl) Record(ctx context.Context, incr int64, options ...metric.RecordOption) {
	y.X.Record(ctx, incr, options...)
}

func EnsureMetricsProvider(mp Provider) Provider {
	if mp == nil {
		return NewNoopMetricsProvider()
	}

	return mp
}
