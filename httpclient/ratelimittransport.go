package httpclient

import (
	"net/http"

	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/ratelimiting"
)

// rateLimitTransport spends a token from a per-host bucket before each request.
type rateLimitTransport struct {
	base    http.RoundTripper
	limiter ratelimiting.RateLimiter
}

var _ http.RoundTripper = (*rateLimitTransport)(nil)

// RoundTrip sends the request if the host's bucket has a token for it.
func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host

	allowed, err := t.limiter.Allow(req.Context(), host)
	if err != nil {
		return nil, errors.Wrapf(err, "consulting the rate limiter for host %q", host)
	}

	if !allowed {
		// Refusals are deliberately left retryable: this transport sits inside
		// the retry loop, so the policy's backoff is what waits for the bucket
		// to refill. Marking it Unretryable would turn a full bucket into a
		// hard failure of the whole request.
		return nil, errors.Wrapf(ratelimiting.ErrRateLimited, "host %q", host)
	}

	return t.base.RoundTrip(req)
}
