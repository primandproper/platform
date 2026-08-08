package httpclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v10/retry"

	"github.com/shoenig/test/must"
)

// newClient builds a client for a test, failing rather than making every call
// site handle an error only a broken metrics provider can produce.
func newClient(t *testing.T, opts ...Option) *http.Client {
	t.Helper()

	client, err := NewHTTPClient(opts...)
	must.NoError(t, err)

	return client
}

// observerForTest builds the observability the transports share, the way
// buildClient does. Every pillar is absent, so it records nowhere — these tests
// are about behavior, and the recording paths only have to not panic.
func observerForTest(t *testing.T) *transportObserver {
	t.Helper()

	obs, err := newTransportObserver(nil, nil, nil)
	must.NoError(t, err)

	return obs
}

// retryTransportForTest builds a retry transport already holding the observer
// buildClient would have given it, for tests that exercise it directly rather
// than through a client.
func retryTransportForTest(t *testing.T, policy retry.Policy, opts ...RetryOption) *retryTransport {
	t.Helper()

	transport := newRetryTransport(policy, opts)
	transport.obs = observerForTest(t)

	return transport
}

// roundTripperFunc adapts a function to http.RoundTripper so a test can state
// the base transport's behavior inline.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// response builds a response the way a real transport would hand one back:
// with a readable, closable body.
func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// withHeader sets a header on a response and returns it, for inline use.
func withHeader(resp *http.Response, key, value string) *http.Response {
	resp.Header.Set(key, value)

	return resp
}

// trackedBody records whether it was closed, which is how the retry tests check
// that superseded responses give their connections back.
type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error {
	b.closed = true

	return nil
}

// immediatePolicy is a retry.Policy that retries up to attempts times with no
// backoff. It honors retry.IsTerminal exactly as the real exponential-backoff
// policy does, which is what makes assertions about ErrUnretryable meaningful.
type immediatePolicy struct {
	attempts int
}

var _ retry.Policy = (*immediatePolicy)(nil)

func (p *immediatePolicy) Execute(ctx context.Context, operation func(context.Context) error) error {
	var err error

	for range p.attempts {
		if err = operation(ctx); err == nil {
			return nil
		}

		if retry.IsTerminal(ctx, err) {
			return err
		}
	}

	return err
}

// classifyRequest is a stand-in for tests that exercise classify directly.
// Classification turns on the response, but the transport still needs a request
// to describe in the lines it logs about one.
func classifyRequest(ctx context.Context) *http.Request {
	return newRequest(ctx, http.MethodGet, "http://example.com", nil)
}

// newRequest builds a request bound to ctx, failing loudly on a bad URL rather
// than making every caller check.
func newRequest(ctx context.Context, method, url string, body io.Reader) *http.Request {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		panic(err)
	}

	return req
}

// get sends a GET bound to ctx. http.Client.Get would do, except that it leaves
// the request on context.Background, and several of these tests turn on what the
// transport does when the caller's context is done.
func get(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	return client.Do(newRequest(ctx, http.MethodGet, url, nil))
}

// post sends a POST bound to ctx.
func post(ctx context.Context, client *http.Client, url string, body io.Reader) (*http.Response, error) {
	req := newRequest(ctx, http.MethodPost, url, body)
	req.Header.Set("Content-Type", "text/plain")

	return client.Do(req)
}
