package httpclient

import (
	"net/http"
	"testing"
	"time"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterHTTPClient(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()

		cfg := &Config{}
		cfg.EnsureDefaults()
		do.ProvideValue(i, cfg)

		test.NotPanic(t, func() {
			RegisterHTTPClient(i)
		})

		client, err := do.Invoke[*http.Client](i)
		must.NoError(t, err)
		test.NotNil(t, client)
	})

	T.Run("with options overriding config", func(t *testing.T) {
		t.Parallel()

		i := do.New()

		cfg := &Config{Timeout: time.Second}
		cfg.EnsureDefaults()
		do.ProvideValue(i, cfg)

		test.NotPanic(t, func() {
			RegisterHTTPClient(i, WithTimeout(9*time.Second))
		})

		client, err := do.Invoke[*http.Client](i)
		must.NoError(t, err)
		must.NotNil(t, client)
		test.EqOp(t, 9*time.Second, client.Timeout)
	})
}
