package webhooks

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v11/cryptography/requestsigning"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// testCatalog is the event catalog the unit tests register against.
var testCatalog = Catalog{
	"order.created": {Description: "an order was created"},
	"order.updated": {Description: "an order was updated"},
}

func TestCheckEndpointURL(T *testing.T) {
	T.Parallel()

	// Literal IPs throughout, so nothing here depends on a DNS lookup.
	T.Run("accepts a public https URL", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, CheckEndpointURL(t.Context(), "https://93.184.216.34/hooks"))
	})

	T.Run("rejects non-https schemes", func(t *testing.T) {
		t.Parallel()

		for _, rawURL := range []string{
			"http://93.184.216.34/hooks",
			"ftp://93.184.216.34/hooks",
			"file:///etc/passwd",
			"gopher://93.184.216.34/",
		} {
			test.ErrorIs(t, CheckEndpointURL(t.Context(), rawURL), ErrInvalidEndpointURL)
		}
	})

	T.Run("rejects a relative or hostless URL", func(t *testing.T) {
		t.Parallel()

		for _, rawURL := range []string{"/hooks", "https://", "://nope"} {
			test.Error(t, CheckEndpointURL(t.Context(), rawURL))
		}
	})

	T.Run("rejects credentials in the URL", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, CheckEndpointURL(t.Context(), "https://user:pass@93.184.216.34/hooks"), ErrInvalidEndpointURL)
	})

	// The SSRF cases. Each of these is a real target someone has been hit with.
	T.Run("rejects non-routable hosts", func(t *testing.T) {
		t.Parallel()

		for name, rawURL := range map[string]string{
			"loopback v4":             "https://127.0.0.1/hooks",
			"loopback v6":             "https://[::1]/hooks",
			"cloud instance metadata": "https://169.254.169.254/latest/meta-data/",
			"link-local v6":           "https://[fe80::1]/hooks",
			"rfc1918 ten":             "https://10.0.0.5/hooks",
			"rfc1918 172":             "https://172.16.3.4/hooks",
			"rfc1918 192.168":         "https://192.168.1.1/hooks",
			"unique local v6":         "https://[fd00::1]/hooks",
			"unspecified":             "https://0.0.0.0/hooks",
			"multicast":               "https://224.0.0.1/hooks",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				test.ErrorIs(t, CheckEndpointURL(t.Context(), rawURL), ErrDisallowedEndpointHost)
			})
		}
	})

	// A port does not change the verdict either way.
	T.Run("honors the host regardless of port", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, CheckEndpointURL(t.Context(), "https://93.184.216.34:8443/hooks"))
		test.ErrorIs(t, CheckEndpointURL(t.Context(), "https://127.0.0.1:8443/hooks"), ErrDisallowedEndpointHost)
	})
}

func TestEndpoint_Validate(T *testing.T) {
	T.Parallel()

	valid := func() *Endpoint {
		return &Endpoint{
			ID:          "endpoint-1",
			URL:         "https://93.184.216.34/hooks",
			ContentType: DefaultContentType,
			Secret:      Secret{Current: []byte("secret")},
			Events:      []string{"order.created"},
		}
	}

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, valid().Validate(t.Context(), testCatalog, nil))
	})

	T.Run("nil endpoint", func(t *testing.T) {
		t.Parallel()

		var endpoint *Endpoint
		test.ErrorIs(t, endpoint.Validate(t.Context(), testCatalog, nil), ErrNilEndpoint)
	})

	T.Run("without a signing secret", func(t *testing.T) {
		t.Parallel()

		endpoint := valid()
		endpoint.Secret = Secret{}

		test.ErrorIs(t, endpoint.Validate(t.Context(), testCatalog, nil), ErrNoSigningSecret)
	})

	T.Run("subscribing to nothing", func(t *testing.T) {
		t.Parallel()

		endpoint := valid()
		endpoint.Events = nil

		test.ErrorIs(t, endpoint.Validate(t.Context(), testCatalog, nil), ErrNoEvents)
	})

	// The typo case the catalog exists to catch. Without this check the endpoint
	// registers cleanly and then never fires.
	T.Run("subscribing to an unknown event", func(t *testing.T) {
		t.Parallel()

		endpoint := valid()
		endpoint.Events = []string{"order.created", "odrer.updated"}

		test.ErrorIs(t, endpoint.Validate(t.Context(), testCatalog, nil), ErrUnknownEventType)
	})

	T.Run("setting a reserved header", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{requestsigning.SignatureHeader, "content-type", "X-PLATFORM-TIMESTAMP"} {
			endpoint := valid()
			endpoint.Headers = map[string]string{name: "attacker-chosen"}

			test.ErrorIs(t, endpoint.Validate(t.Context(), testCatalog, nil), ErrReservedHeader)
		}
	})

	T.Run("permits ordinary static headers", func(t *testing.T) {
		t.Parallel()

		endpoint := valid()
		endpoint.Headers = map[string]string{"X-Tenant": "acme"}

		test.NoError(t, endpoint.Validate(t.Context(), testCatalog, nil))
	})

	T.Run("honors a replacement URL checker", func(t *testing.T) {
		t.Parallel()

		endpoint := valid()
		endpoint.URL = "http://127.0.0.1:9000/hooks"

		must.Error(t, endpoint.Validate(t.Context(), testCatalog, nil))
		test.NoError(t, endpoint.Validate(t.Context(), testCatalog, func(context.Context, string) error { return nil }))
	})
}

func TestEndpoint_applyHeaders(T *testing.T) {
	T.Parallel()

	T.Run("writes static headers", func(t *testing.T) {
		t.Parallel()

		endpoint := &Endpoint{Headers: map[string]string{"X-Tenant": "acme"}}

		header := http.Header{}
		endpoint.applyHeaders(header)

		test.EqOp(t, "acme", header.Get("X-Tenant"))
	})

	// Registration rejects these, but a Store implementation this package did
	// not validate can hand one back — and a subscriber that could set its own
	// signature header would be authenticating against a value it chose.
	T.Run("refuses to overwrite a reserved header", func(t *testing.T) {
		t.Parallel()

		endpoint := &Endpoint{Headers: map[string]string{
			"x-platform-signature": "forged",
			"Content-Type":         "text/plain",
			"X-Tenant":             "acme",
		}}

		header := http.Header{}
		endpoint.applyHeaders(header)

		test.EqOp(t, "", header.Get(requestsigning.SignatureHeader))
		test.EqOp(t, "", header.Get("Content-Type"))
		test.EqOp(t, "acme", header.Get("X-Tenant"))
	})
}

func TestEndpoint_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		endpoint := &Endpoint{}
		endpoint.EnsureDefaults()

		test.EqOp(t, DefaultContentType, endpoint.ContentType)
	})

	T.Run("leaves an explicit content type alone", func(t *testing.T) {
		t.Parallel()

		endpoint := &Endpoint{ContentType: "application/cloudevents+json"}
		endpoint.EnsureDefaults()

		test.EqOp(t, "application/cloudevents+json", endpoint.ContentType)
	})

	T.Run("nil endpoint does not panic", func(t *testing.T) {
		t.Parallel()

		var endpoint *Endpoint
		endpoint.EnsureDefaults()
	})
}

func TestCatalog(T *testing.T) {
	T.Parallel()

	T.Run("Known", func(t *testing.T) {
		t.Parallel()

		test.True(t, testCatalog.Known("order.created"))
		test.False(t, testCatalog.Known("order.deleted"))
		test.False(t, Catalog(nil).Known("order.created"))
	})

	T.Run("EventTypes is sorted", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, []string{"order.created", "order.updated"}, testCatalog.EventTypes())
	})

	T.Run("EventTypes of an empty catalog", func(t *testing.T) {
		t.Parallel()

		test.SliceEmpty(t, Catalog{}.EventTypes())
	})
}

func TestAttempt_Succeeded(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.True(t, (&Attempt{StatusCode: 200}).Succeeded())
		test.True(t, (&Attempt{StatusCode: 204}).Succeeded())
		test.False(t, (&Attempt{StatusCode: 500}).Succeeded())
		test.False(t, (&Attempt{StatusCode: 200, Error: "boom"}).Succeeded())
	})

	// A redirect is not success. The client refuses to follow it, so treating it
	// as delivered would silently drop the payload.
	T.Run("a redirect is not success", func(t *testing.T) {
		t.Parallel()

		test.False(t, (&Attempt{StatusCode: 302}).Succeeded())
	})
}

func TestTerminalStatus(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		// The subscriber understood and refused; retrying changes nothing.
		test.True(t, terminalStatus(http.StatusBadRequest))
		test.True(t, terminalStatus(http.StatusUnauthorized))
		test.True(t, terminalStatus(http.StatusNotFound))
		test.True(t, terminalStatus(http.StatusGone))

		// Both of these explicitly invite a later attempt.
		test.False(t, terminalStatus(http.StatusRequestTimeout))
		test.False(t, terminalStatus(http.StatusTooManyRequests))

		// Server-side failures are transient until proven otherwise.
		test.False(t, terminalStatus(http.StatusInternalServerError))
		test.False(t, terminalStatus(http.StatusBadGateway))
		test.False(t, terminalStatus(http.StatusServiceUnavailable))

		test.False(t, terminalStatus(http.StatusOK))
	})
}

// The hostname path, which every other case in this file skips by using literal
// IPs. It is the branch that actually runs at delivery time for a real
// subscriber, so leaving it unexercised would mean the resolver loop was never
// executed by any test.
func TestCheckEndpointURL_resolution(T *testing.T) {
	T.Parallel()

	// localhost resolves without touching the network and lands on loopback,
	// which is exactly what the guard must refuse.
	T.Run("rejects a name that resolves to loopback", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, CheckEndpointURL(t.Context(), "https://localhost/hooks"), ErrDisallowedEndpointHost)
	})

	// A cancelled context fails the lookup deterministically, without depending
	// on a DNS server being unreachable.
	T.Run("surfaces a resolution failure", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		test.ErrorIs(t, CheckEndpointURL(ctx, "https://example.com/hooks"), ErrInvalidEndpointURL)
	})

	// The DNS lookup is what makes the delivery-time re-check able to hang, so
	// it has to honor the deadline it is given.
	T.Run("honors a deadline", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
		defer cancel()

		test.Error(t, CheckEndpointURL(ctx, "https://example.com/hooks"))
	})
}

func TestCheckIP(T *testing.T) {
	T.Parallel()

	T.Run("accepts a globally routable address", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, checkIP(net.ParseIP("93.184.216.34"), "example.com"))
		test.NoError(t, checkIP(net.ParseIP("2606:2800:220:1:248:1893:25c8:1946"), "example.com"))
	})

	T.Run("rejects everything else", func(t *testing.T) {
		t.Parallel()

		for name, ip := range map[string]string{
			"loopback v4":     "127.0.0.1",
			"loopback v6":     "::1",
			"link-local v4":   "169.254.169.254",
			"link-local v6":   "fe80::1",
			"private 10":      "10.0.0.1",
			"private 172":     "172.20.0.1",
			"private 192.168": "192.168.0.1",
			"unique local v6": "fd12::1",
			"unspecified v4":  "0.0.0.0",
			"unspecified v6":  "::",
			"multicast v4":    "239.0.0.1",
			"multicast v6":    "ff02::1",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				test.ErrorIs(t, checkIP(net.ParseIP(ip), "host"), ErrDisallowedEndpointHost)
			})
		}
	})
}
