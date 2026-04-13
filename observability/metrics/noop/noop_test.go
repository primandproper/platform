package noop

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewMetricsProvider(T *testing.T) {
	T.Parallel()

	T.Run("returns non-nil provider", func(t *testing.T) {
		t.Parallel()

		p := NewMetricsProvider()
		must.NotNil(t, p)
	})
}

func TestMetricsProvider_NewFloat64Counter(T *testing.T) {
	T.Parallel()

	T.Run("returns counter and no error", func(t *testing.T) {
		t.Parallel()

		p := NewMetricsProvider()
		c, err := p.NewFloat64Counter("test_counter")

		must.NoError(t, err)
		must.NotNil(t, c)
	})
}

func TestMetricsProvider_NewFloat64Gauge(T *testing.T) {
	T.Parallel()

	T.Run("returns gauge and no error", func(t *testing.T) {
		t.Parallel()

		p := NewMetricsProvider()
		g, err := p.NewFloat64Gauge("test_gauge")

		must.NoError(t, err)
		must.NotNil(t, g)
	})
}

func TestMetricsProvider_NewFloat64UpDownCounter(T *testing.T) {
	T.Parallel()

	T.Run("returns counter and no error", func(t *testing.T) {
		t.Parallel()

		p := NewMetricsProvider()
		c, err := p.NewFloat64UpDownCounter("test_updown")

		must.NoError(t, err)
		must.NotNil(t, c)
	})
}

func TestMetricsProvider_NewFloat64Histogram(T *testing.T) {
	T.Parallel()

	T.Run("returns histogram and no error", func(t *testing.T) {
		t.Parallel()

		p := NewMetricsProvider()
		h, err := p.NewFloat64Histogram("test_histogram")

		must.NoError(t, err)
		must.NotNil(t, h)
	})
}

func TestMetricsProvider_NewInt64Counter(T *testing.T) {
	T.Parallel()

	T.Run("returns counter and no error", func(t *testing.T) {
		t.Parallel()

		p := NewMetricsProvider()
		c, err := p.NewInt64Counter("test_counter")

		must.NoError(t, err)
		must.NotNil(t, c)
	})
}

func TestMetricsProvider_NewInt64Gauge(T *testing.T) {
	T.Parallel()

	T.Run("returns gauge and no error", func(t *testing.T) {
		t.Parallel()

		p := NewMetricsProvider()
		g, err := p.NewInt64Gauge("test_gauge")

		must.NoError(t, err)
		must.NotNil(t, g)
	})
}

func TestMetricsProvider_NewInt64UpDownCounter(T *testing.T) {
	T.Parallel()

	T.Run("returns counter and no error", func(t *testing.T) {
		t.Parallel()

		p := NewMetricsProvider()
		c, err := p.NewInt64UpDownCounter("test_updown")

		must.NoError(t, err)
		must.NotNil(t, c)
	})
}

func TestMetricsProvider_NewInt64Histogram(T *testing.T) {
	T.Parallel()

	T.Run("returns histogram and no error", func(t *testing.T) {
		t.Parallel()

		p := NewMetricsProvider()
		h, err := p.NewInt64Histogram("test_histogram")

		must.NoError(t, err)
		must.NotNil(t, h)
	})
}

func TestMetricsProvider_MeterProvider(T *testing.T) {
	T.Parallel()

	T.Run("returns non-nil meter provider", func(t *testing.T) {
		t.Parallel()

		p := NewMetricsProvider()
		test.NotNil(t, p.MeterProvider())
	})
}

func TestMetricsProvider_Shutdown(T *testing.T) {
	T.Parallel()

	T.Run("returns no error", func(t *testing.T) {
		t.Parallel()

		p := NewMetricsProvider()
		test.NoError(t, p.Shutdown(t.Context()))
	})
}
