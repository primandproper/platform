package httpclient

import (
	"errors"
	"net/http"

	"github.com/primandproper/platform-go/v9/circuitbreaking"
	"github.com/primandproper/platform-go/v9/circuitbreaking/partitioned"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/ratelimiting"
)

// breakerTransport consults a per-host circuit breaker before spending a
// connection on a dependency that has been failing.
type breakerTransport struct {
	base     http.RoundTripper
	breakers partitioned.KeyedCircuitBreaker
}

var _ http.RoundTripper = (*breakerTransport)(nil)

// RoundTrip fails fast when the host's circuit is open, and reports the outcome
// of every request it does let through.
func (t *breakerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host

	breaker := t.breakers.For(host)
	if breaker == nil {
		return t.base.RoundTrip(req)
	}

	if breaker.CannotProceed() {
		// Wrapped rather than replaced: errors/http already maps
		// ErrCircuitBroken, and the host is what makes the log line useful.
		return nil, platformerrors.Wrapf(circuitbreaking.ErrCircuitBroken, "host %q", host)
	}

	resp, err := t.base.RoundTrip(req)

	// A request this client's own limiter refused never reached the host, so it
	// is evidence about the local budget and none at all about the dependency's
	// health. Counting it would let ordinary throttling trip a circuit against a
	// host that is perfectly well — and then keep it tripped, since the refusals
	// continue whether or not the host recovers.
	if errors.Is(err, ratelimiting.ErrRateLimited) {
		return resp, err
	}

	// A 5xx counts against the host for the same reason a dial failure does:
	// the dependency is unwell, and the point of the breaker is to stop asking.
	// A 4xx does not — it says this request was wrong, which is the caller's
	// problem and no reason to cut off every other caller of the same host.
	if err != nil || resp.StatusCode >= http.StatusInternalServerError {
		breaker.Failed()
	} else {
		breaker.Succeeded()
	}

	return resp, err
}
