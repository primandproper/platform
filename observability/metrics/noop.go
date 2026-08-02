package metrics

import (
	"context"

	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
)

// noopMeter is a genuinely no-op meter.
//
// otel.Meter (the process-global provider) would make these "noop" instruments
// record and export real metrics the moment anything installs a real global
// provider — which this repo's own otelgrpc setup does — so EnsureMetricsProvider's
// fallback was the opposite of a noop, and behaved differently from the
// metrics/noop package that documents this exact hazard.
var noopMeter = metricnoop.NewMeterProvider().Meter("noop")

type noopProvider struct{}

// NewFloat64Counter is a no-op method.
func (m *noopProvider) NewFloat64Counter(name string, options ...metric.Float64CounterOption) (Float64Counter, error) {
	y, err := noopMeter.Float64Counter(name, options...)
	if err != nil {
		return nil, err
	}

	x := &Float64CounterImpl{
		X: y,
	}

	return x, nil
}

// NewFloat64Gauge is a no-op method.
func (m *noopProvider) NewFloat64Gauge(name string, options ...metric.Float64GaugeOption) (Float64Gauge, error) {
	y, err := noopMeter.Float64Gauge(name, options...)
	if err != nil {
		return nil, err
	}

	x := &Float64GaugeImpl{X: y}

	return x, nil
}

// NewFloat64UpDownCounter is a no-op method.
func (m *noopProvider) NewFloat64UpDownCounter(name string, options ...metric.Float64UpDownCounterOption) (Float64UpDownCounter, error) {
	y, err := noopMeter.Float64UpDownCounter(name, options...)
	if err != nil {
		return nil, err
	}

	x := &Float64UpDownCounterImpl{X: y}

	return x, nil
}

// NewFloat64Histogram is a no-op method.
func (m *noopProvider) NewFloat64Histogram(name string, options ...metric.Float64HistogramOption) (Float64Histogram, error) {
	y, err := noopMeter.Float64Histogram(name, options...)
	if err != nil {
		return nil, err
	}

	x := &Float64HistogramImpl{X: y}

	return x, nil
}

// NewInt64Counter is a no-op method.
func (m *noopProvider) NewInt64Counter(name string, options ...metric.Int64CounterOption) (Int64Counter, error) {
	y, err := noopMeter.Int64Counter(name, options...)
	if err != nil {
		return nil, err
	}

	x := &Int64CounterImpl{X: y}

	return x, nil
}

// NewInt64Gauge is a no-op method.
func (m *noopProvider) NewInt64Gauge(name string, options ...metric.Int64GaugeOption) (Int64Gauge, error) {
	y, err := noopMeter.Int64Gauge(name, options...)
	if err != nil {
		return nil, err
	}

	x := &Int64GaugeImpl{X: y}

	return x, nil
}

// NewInt64UpDownCounter is a no-op method.
func (m *noopProvider) NewInt64UpDownCounter(name string, options ...metric.Int64UpDownCounterOption) (Int64UpDownCounter, error) {
	y, err := noopMeter.Int64UpDownCounter(name, options...)
	if err != nil {
		return nil, err
	}

	x := &Int64UpDownCounterImpl{X: y}

	return x, nil
}

// NewInt64Histogram is a no-op method.
func (m *noopProvider) NewInt64Histogram(name string, options ...metric.Int64HistogramOption) (Int64Histogram, error) {
	y, err := noopMeter.Int64Histogram(name, options...)
	if err != nil {
		return nil, err
	}

	x := &Int64HistogramImpl{X: y}

	return x, nil
}

// MeterProvider satisfies our interface. Returns the otel noop MeterProvider so
// consumers (e.g. otelsql) never receive nil.
func (m *noopProvider) MeterProvider() metric.MeterProvider {
	return metricnoop.NewMeterProvider()
}

// Shutdown satisfies our interface.
func (m *noopProvider) Shutdown(ctx context.Context) error {
	return nil
}
