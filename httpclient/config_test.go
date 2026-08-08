package httpclient

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("sets defaults for zero values", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		test.EqOp(t, defaultTimeout, cfg.Timeout)
		test.EqOp(t, defaultMaxIdleConns, cfg.MaxIdleConns)
		test.EqOp(t, defaultMaxIdleConnsPerHost, cfg.MaxIdleConnsPerHost)
	})

	T.Run("preserves non-zero values", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Timeout:             5 * time.Second,
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 25,
		}
		cfg.EnsureDefaults()

		test.EqOp(t, 5*time.Second, cfg.Timeout)
		test.EqOp(t, 50, cfg.MaxIdleConns)
		test.EqOp(t, 25, cfg.MaxIdleConnsPerHost)
	})
}

func TestConfig_Options(T *testing.T) {
	T.Parallel()

	T.Run("nil config yields no options", func(t *testing.T) {
		t.Parallel()

		var cfg *Config

		test.SliceEmpty(t, cfg.Options())

		client := newClient(t, cfg.Options()...)
		must.NotNil(t, client)
		test.EqOp(t, defaultTimeout, client.Timeout)
	})

	T.Run("config values reach the client", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Timeout:             7 * time.Second,
			MaxIdleConns:        13,
			MaxIdleConnsPerHost: 7,
		}

		client := newClient(t, cfg.Options()...)
		must.NotNil(t, client)
		test.EqOp(t, 7*time.Second, client.Timeout)

		transport, ok := client.Transport.(*http.Transport)
		must.True(t, ok)
		test.EqOp(t, 13, transport.MaxIdleConns)
		test.EqOp(t, 7, transport.MaxIdleConnsPerHost)
	})

	T.Run("zero-valued fields keep defaults", func(t *testing.T) {
		t.Parallel()

		client := newClient(t, (&Config{}).Options()...)
		must.NotNil(t, client)
		test.EqOp(t, defaultTimeout, client.Timeout)

		transport, ok := client.Transport.(*http.Transport)
		must.True(t, ok)
		test.EqOp(t, defaultMaxIdleConns, transport.MaxIdleConns)
		test.EqOp(t, defaultMaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
	})

	T.Run("enables tracing", func(t *testing.T) {
		t.Parallel()

		client := newClient(t, (&Config{EnableTracing: true}).Options()...)
		must.NotNil(t, client)

		_, ok := client.Transport.(*http.Transport)
		test.False(t, ok)
	})

	T.Run("appended options override the config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Timeout: 7 * time.Second}

		client := newClient(t, append(cfg.Options(), WithTimeout(11*time.Second))...)
		must.NotNil(t, client)
		test.EqOp(t, 11*time.Second, client.Timeout)
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("valid config", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		cfg := &Config{
			Timeout:             time.Second,
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
		}

		err := cfg.ValidateWithContext(ctx)
		must.NoError(t, err)
	})

	T.Run("invalid timeout", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		cfg := &Config{
			Timeout:             0,
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
		}

		err := cfg.ValidateWithContext(ctx)
		must.Error(t, err)
	})

	T.Run("invalid MaxIdleConns", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		cfg := &Config{
			Timeout:             time.Second,
			MaxIdleConns:        0,
			MaxIdleConnsPerHost: 5,
		}

		err := cfg.ValidateWithContext(ctx)
		must.Error(t, err)
	})

	T.Run("invalid MaxIdleConnsPerHost", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		cfg := &Config{
			Timeout:             time.Second,
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 0,
		}

		err := cfg.ValidateWithContext(ctx)
		must.Error(t, err)
	})
}
