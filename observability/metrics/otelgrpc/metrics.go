package otelgrpc

import (
	"context"
	"fmt"
	"time"

	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	o11yutils "github.com/primandproper/platform-go/v10/observability/utils"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"go.opentelemetry.io/contrib/instrumentation/host"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
)

var (
	ErrNilConfig = errors.New("nil config")
)

type Config struct {
	CollectorEndpoint    string        `env:"COLLECTOR_ENDPOINT"     json:"metricsCollectorEndpoint,omitempty" yaml:"metricsCollectorEndpoint,omitempty"`
	CollectionInterval   time.Duration `env:"COLLECTION_INTERVAL"    json:"collectionInterval,omitempty"       yaml:"collectionInterval,omitempty"`
	Insecure             bool          `env:"INSECURE"               json:"insecure,omitempty"                 yaml:"insecure,omitempty"`
	EnableRuntimeMetrics bool          `env:"ENABLE_RUNTIME_METRICS" json:"enableRuntimeMetrics,omitempty"     yaml:"enableRuntimeMetrics,omitempty"`
	EnableHostMetrics    bool          `env:"ENABLE_HOST_METRICS"    json:"enableHostMetrics,omitempty"        yaml:"enableHostMetrics,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates the config struct.
func (c *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, c,
		validation.Field(&c.CollectorEndpoint, validation.Required),
		validation.Field(&c.CollectionInterval, validation.Required),
	)
}

func setupMetricsProvider(ctx context.Context, logger logging.Logger, serviceName string, cfg *Config) (metric.MeterProvider, func(context.Context) error, error) {
	if cfg == nil {
		return nil, nil, ErrNilConfig
	}

	res, err := o11yutils.OtelResource(ctx, serviceName)
	if err != nil {
		return nil, nil, err
	}

	options := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(cfg.CollectorEndpoint),
	}

	if cfg.Insecure {
		logger.Info("using insecure connection to metrics collector")
		options = append(options, otlpmetricgrpc.WithInsecure())
	}

	metricExp, err := otlpmetricgrpc.New(
		ctx,
		options...,
	)
	if err != nil {
		return nil, nil, errors.Wrap(err, "setting up metrics exporter")
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithExemplarFilter(exemplar.AlwaysOnFilter),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				metricExp,
				sdkmetric.WithInterval(cfg.CollectionInterval),
			),
		),
	)

	// Registering the global provider is NewMetricsProvider's job, once, after
	// this returns. Doing it here as well meant every setup assigned the same
	// process-global twice.

	logger.WithValue("config", cfg).Info("set up meter provider")

	if cfg.EnableRuntimeMetrics {
		if err = runtime.Start(runtime.WithMeterProvider(meterProvider)); err != nil {
			return nil, nil, errors.Wrap(err, "starting runtime metrics")
		}
		logger.Info("started runtime metrics")
	}

	if cfg.EnableHostMetrics {
		if err = host.Start(host.WithMeterProvider(meterProvider)); err != nil {
			return nil, nil, errors.Wrap(err, "starting host metrics")
		}
		logger.Info("started host metrics")
	}

	return meterProvider, meterProvider.Shutdown, nil
}

func NewMetricsProvider(ctx context.Context, logger logging.Logger, serviceName string, cfg *Config) (metrics.Provider, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}

	logger.WithValue("service.name", serviceName).
		WithValue("interval", cfg.CollectionInterval.String()).
		Info("setting up metrics provider")

	meterProvider, shutdown, err := setupMetricsProvider(ctx, logger, serviceName, cfg)
	if err != nil {
		return nil, errors.Wrap(err, "creating metric provider")
	}

	// Set the global meter provider
	otel.SetMeterProvider(meterProvider)

	i := &providerImpl{
		logger:        logging.EnsureLogger(logger),
		serviceName:   serviceName,
		meterProvider: meterProvider,
		mp:            meterProvider.Meter(serviceName),
		shutdownFunctions: []func(context.Context) error{
			shutdown,
		},
	}

	return i, nil
}

var _ metrics.Provider = (*providerImpl)(nil)

type providerImpl struct {
	mp                metric.Meter
	meterProvider     metric.MeterProvider
	logger            logging.Logger
	serviceName       string
	shutdownFunctions []func(context.Context) error
}

func (m *providerImpl) MeterProvider() metric.MeterProvider {
	return m.meterProvider
}

func (m *providerImpl) Shutdown(ctx context.Context) error {
	errs := make([]error, 0, len(m.shutdownFunctions))

	for _, fn := range m.shutdownFunctions {
		if err := fn(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// qualify prefixes an instrument name with the service name and records the
// creation at Debug.
//
// Each of these was an Info line. A process registers its instruments while
// starting up — around fifty of them across this repo's packages — so the first
// thing an operator saw in the log was fifty lines reporting that metrics
// plumbing had been plumbed, ahead of anything the service had actually done.
func (m *providerImpl) qualify(name, kind string) string {
	m.logger.WithValues(map[string]any{
		"instrument.name": name,
		"instrument.kind": kind,
	}).Debug("creating instrument")

	return fmt.Sprintf("%s.%s", m.serviceName, name)
}

func (m *providerImpl) NewFloat64Counter(name string, options ...metric.Float64CounterOption) (metrics.Float64Counter, error) {
	z, err := m.mp.Float64Counter(m.qualify(name, "float64_counter"), options...)
	if err != nil {
		return nil, errors.Wrap(err, "creating float64_counter instrument")
	}

	return &metrics.Float64CounterImpl{X: z}, nil
}

func (m *providerImpl) NewFloat64Gauge(name string, options ...metric.Float64GaugeOption) (metrics.Float64Gauge, error) {
	z, err := m.mp.Float64Gauge(m.qualify(name, "float64_gauge"), options...)
	if err != nil {
		return nil, errors.Wrap(err, "creating float64_gauge instrument")
	}

	return &metrics.Float64GaugeImpl{X: z}, nil
}

func (m *providerImpl) NewFloat64UpDownCounter(name string, options ...metric.Float64UpDownCounterOption) (metrics.Float64UpDownCounter, error) {
	z, err := m.mp.Float64UpDownCounter(m.qualify(name, "float64_up_down_counter"), options...)
	if err != nil {
		return nil, errors.Wrap(err, "creating float64_up_down_counter instrument")
	}

	return &metrics.Float64UpDownCounterImpl{X: z}, nil
}

func (m *providerImpl) NewFloat64Histogram(name string, options ...metric.Float64HistogramOption) (metrics.Float64Histogram, error) {
	z, err := m.mp.Float64Histogram(m.qualify(name, "float64_histogram"), options...)
	if err != nil {
		return nil, errors.Wrap(err, "creating float64_histogram instrument")
	}

	return &metrics.Float64HistogramImpl{X: z}, nil
}

func (m *providerImpl) NewInt64Counter(name string, options ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
	z, err := m.mp.Int64Counter(m.qualify(name, "int64_counter"), options...)
	if err != nil {
		return nil, errors.Wrap(err, "creating int64_counter instrument")
	}

	return &metrics.Int64CounterImpl{X: z}, nil
}

func (m *providerImpl) NewInt64Gauge(name string, options ...metric.Int64GaugeOption) (metrics.Int64Gauge, error) {
	z, err := m.mp.Int64Gauge(m.qualify(name, "int64_gauge"), options...)
	if err != nil {
		return nil, errors.Wrap(err, "creating int64_gauge instrument")
	}

	return &metrics.Int64GaugeImpl{X: z}, nil
}

func (m *providerImpl) NewInt64UpDownCounter(name string, options ...metric.Int64UpDownCounterOption) (metrics.Int64UpDownCounter, error) {
	z, err := m.mp.Int64UpDownCounter(m.qualify(name, "int64_up_down_counter"), options...)
	if err != nil {
		return nil, errors.Wrap(err, "creating int64_up_down_counter instrument")
	}

	return &metrics.Int64UpDownCounterImpl{X: z}, nil
}

func (m *providerImpl) NewInt64Histogram(name string, options ...metric.Int64HistogramOption) (metrics.Int64Histogram, error) {
	z, err := m.mp.Int64Histogram(m.qualify(name, "int64_histogram"), options...)
	if err != nil {
		return nil, errors.Wrap(err, "creating int64_histogram instrument")
	}

	return &metrics.Int64HistogramImpl{X: z}, nil
}
