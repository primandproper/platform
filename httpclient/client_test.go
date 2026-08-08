package httpclient

import (
	"net/http"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// stubRoundTripper is a RoundTripper that records nothing and returns nothing; it
// exists to assert that WithTransport is honored.
type stubRoundTripper struct{}

func (stubRoundTripper) RoundTrip(*http.Request) (*http.Response, error) { return nil, nil }

func TestNewHTTPClient(T *testing.T) {
	T.Parallel()

	T.Run("with no options uses defaults", func(t *testing.T) {
		t.Parallel()

		client := newClient(t)
		must.NotNil(t, client)
		test.EqOp(t, defaultTimeout, client.Timeout)

		transport, ok := client.Transport.(*http.Transport)
		must.True(t, ok)
		test.EqOp(t, defaultMaxIdleConns, transport.MaxIdleConns)
		test.EqOp(t, defaultMaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
	})

	T.Run("with tracing enabled", func(t *testing.T) {
		t.Parallel()

		client := newClient(t, WithTimeout(2*time.Second), WithTracing(true))
		must.NotNil(t, client)
		test.EqOp(t, 2*time.Second, client.Timeout)
		must.NotNil(t, client.Transport)

		_, ok := client.Transport.(*http.Transport)
		test.False(t, ok)
	})

	T.Run("with tracing disabled", func(t *testing.T) {
		t.Parallel()

		client := newClient(t, WithTimeout(3*time.Second), WithTracing(false))
		must.NotNil(t, client)
		test.EqOp(t, 3*time.Second, client.Timeout)

		_, ok := client.Transport.(*http.Transport)
		test.True(t, ok)
	})

	T.Run("applies connection pool options", func(t *testing.T) {
		t.Parallel()

		client := newClient(t,
			WithTimeout(time.Second),
			WithMaxIdleConns(42),
			WithMaxIdleConnsPerHost(21),
		)
		must.NotNil(t, client)

		transport, ok := client.Transport.(*http.Transport)
		must.True(t, ok)
		test.EqOp(t, 42, transport.MaxIdleConns)
		test.EqOp(t, 21, transport.MaxIdleConnsPerHost)
	})

	T.Run("ignores non-positive option values", func(t *testing.T) {
		t.Parallel()

		client := newClient(t,
			WithTimeout(0),
			WithMaxIdleConns(0),
			WithMaxIdleConnsPerHost(-1),
			WithTransport(nil),
		)
		must.NotNil(t, client)
		test.EqOp(t, defaultTimeout, client.Timeout)

		transport, ok := client.Transport.(*http.Transport)
		must.True(t, ok)
		test.EqOp(t, defaultMaxIdleConns, transport.MaxIdleConns)
		test.EqOp(t, defaultMaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
	})

	T.Run("later options override earlier ones", func(t *testing.T) {
		t.Parallel()

		client := newClient(t, WithTimeout(time.Second), WithTimeout(5*time.Second))
		must.NotNil(t, client)
		test.EqOp(t, 5*time.Second, client.Timeout)
	})

	T.Run("with transport", func(t *testing.T) {
		t.Parallel()

		client := newClient(t, WithTransport(stubRoundTripper{}))
		must.NotNil(t, client)

		_, ok := client.Transport.(stubRoundTripper)
		test.True(t, ok)
	})

	T.Run("with transport still wraps in tracing", func(t *testing.T) {
		t.Parallel()

		client := newClient(t, WithTransport(stubRoundTripper{}), WithTracing(true))
		must.NotNil(t, client)

		_, ok := client.Transport.(stubRoundTripper)
		test.False(t, ok)
	})
}
