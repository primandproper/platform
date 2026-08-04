package webhooks

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// publicAddr is a globally routable address every check in this file accepts.
const publicAddr = "93.184.216.34"

// metadataAddr is the cloud instance metadata service — the destination a
// rebinding attack is trying to reach.
const metadataAddr = "169.254.169.254"

// rebindResolver is the DNS rebinding attack in full: the first lookup answers
// with a public address, so registration and the delivery-time check both pass,
// and every lookup after it answers with the metadata service. An attacker who
// controls the authoritative server for a name they registered has exactly this
// much power, and it is not a large amount of power to have.
type rebindResolver struct {
	calls atomic.Int64
}

var _ Resolver = (*rebindResolver)(nil)

func (r *rebindResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	if r.calls.Add(1) == 1 {
		return []net.IPAddr{{IP: net.ParseIP(publicAddr)}}, nil
	}

	return []net.IPAddr{{IP: net.ParseIP(metadataAddr)}}, nil
}

// recordingDialer answers every dial from the same listener, recording the
// address it was asked for. It is how these tests observe where a transport
// intended to connect without depending on the network to get there.
type recordingDialer struct {
	asked atomic.Value

	target string
}

func (d *recordingDialer) dial(ctx context.Context, network, address string) (net.Conn, error) {
	d.asked.Store(address)

	return (&net.Dialer{}).DialContext(ctx, network, d.target)
}

func (d *recordingDialer) asked1(t *testing.T) string {
	t.Helper()

	asked, ok := d.asked.Load().(string)
	must.True(t, ok, must.Sprint("nothing was dialed"))

	return asked
}

// unpinnableTransport is a RoundTripper that is not an *http.Transport — the
// case pinningTransport cannot reach into.
type unpinnableTransport struct{}

func (unpinnableTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, http.ErrNotSupported
}

func TestRebindResolver(T *testing.T) {
	T.Parallel()

	// The stub is load-bearing enough to deserve its own assertion: if it did
	// not actually change its answer, every test below would pass vacuously.
	T.Run("answers publicly once and privately after", func(t *testing.T) {
		t.Parallel()

		resolver := &rebindResolver{}
		check := NewEndpointURLChecker(resolver)

		addrs, err := check(t.Context(), "https://rebind.example.com/hooks")
		must.NoError(t, err)
		test.Eq(t, []netip.Addr{netip.MustParseAddr(publicAddr)}, addrs)

		// The same URL, the same checker, moments later.
		_, err = check(t.Context(), "https://rebind.example.com/hooks")
		test.ErrorIs(t, err, ErrDisallowedEndpointHost)
	})
}

func TestWithPinnedAddrs(T *testing.T) {
	T.Parallel()

	T.Run("round trips", func(t *testing.T) {
		t.Parallel()

		want := []netip.Addr{netip.MustParseAddr(publicAddr)}

		host, addrs := pinnedAddrs(withPinnedAddrs(t.Context(), "example.com", want))
		test.EqOp(t, "example.com", host)
		test.Eq(t, want, addrs)
	})

	// Nothing to pin must mean an unpinned dial, not a dial pinned to nothing —
	// the second would refuse every delivery a permissive checker approved.
	T.Run("an empty set does not pin", func(t *testing.T) {
		t.Parallel()

		_, addrs := pinnedAddrs(withPinnedAddrs(t.Context(), "example.com", nil))
		test.SliceEmpty(t, addrs)
	})

	// A host with no name to hold it to is a pin the dialer cannot apply, since
	// it could not tell the endpoint's connection from a proxy's.
	T.Run("an empty host does not pin", func(t *testing.T) {
		t.Parallel()

		_, addrs := pinnedAddrs(withPinnedAddrs(t.Context(), "", []netip.Addr{netip.MustParseAddr(publicAddr)}))
		test.SliceEmpty(t, addrs)
	})

	T.Run("a bare context pins nothing", func(t *testing.T) {
		t.Parallel()

		host, addrs := pinnedAddrs(t.Context())
		test.EqOp(t, "", host)
		test.SliceEmpty(t, addrs)
	})

	// The pin outlives the call that set it, so aliasing the caller's slice
	// would leave the destination set editable after it was approved.
	T.Run("does not alias the caller's slice", func(t *testing.T) {
		t.Parallel()

		addrs := []netip.Addr{netip.MustParseAddr(publicAddr)}
		ctx := withPinnedAddrs(t.Context(), "example.com", addrs)

		addrs[0] = netip.MustParseAddr(metadataAddr)

		_, pinned := pinnedAddrs(ctx)
		must.SliceLen(t, 1, pinned)
		test.EqOp(t, netip.MustParseAddr(publicAddr), pinned[0])
	})
}

func TestPinningDialContext(T *testing.T) {
	T.Parallel()

	// The point of the whole exercise: the transport asks for the name, and the
	// dialer underneath connects to the address the check approved.
	T.Run("dials the pinned address instead of the requested host", func(t *testing.T) {
		t.Parallel()

		var asked string

		dial := PinningDialContext(func(_ context.Context, _, address string) (net.Conn, error) {
			asked = address

			return nil, http.ErrNotSupported
		})

		ctx := withPinnedAddrs(t.Context(), "rebind.example.com", []netip.Addr{netip.MustParseAddr(publicAddr)})

		_, err := dial(ctx, "tcp", "rebind.example.com:443")
		test.Error(t, err)
		test.EqOp(t, publicAddr+":443", asked)
	})

	// The port comes from the address the transport asked for, since that is the
	// part of it the check never had an opinion about.
	T.Run("keeps the requested port", func(t *testing.T) {
		t.Parallel()

		var asked string

		dial := PinningDialContext(func(_ context.Context, _, address string) (net.Conn, error) {
			asked = address

			return nil, http.ErrNotSupported
		})

		ctx := withPinnedAddrs(t.Context(), "rebind.example.com", []netip.Addr{netip.MustParseAddr("2606:2800:220:1:248:1893:25c8:1946")})

		_, err := dial(ctx, "tcp", "rebind.example.com:8443")
		test.Error(t, err)
		test.EqOp(t, "[2606:2800:220:1:248:1893:25c8:1946]:8443", asked)
	})

	// A client shared with a health probe or anything else the caller reaches
	// for still dials normally; the pin is a property of the delivery.
	T.Run("passes an unpinned dial straight through", func(t *testing.T) {
		t.Parallel()

		var asked string

		dial := PinningDialContext(func(_ context.Context, _, address string) (net.Conn, error) {
			asked = address

			return nil, http.ErrNotSupported
		})

		_, err := dial(t.Context(), "tcp", "example.com:443")
		test.Error(t, err)
		test.EqOp(t, "example.com:443", asked)
	})

	T.Run("tries every pinned address until one connects", func(t *testing.T) {
		t.Parallel()

		var asked []string

		dial := PinningDialContext(func(_ context.Context, _, address string) (net.Conn, error) {
			asked = append(asked, address)

			if len(asked) < 2 {
				return nil, http.ErrNotSupported
			}

			return nil, nil
		})

		ctx := withPinnedAddrs(t.Context(), "example.com", []netip.Addr{
			netip.MustParseAddr(publicAddr),
			netip.MustParseAddr("93.184.216.35"),
		})

		_, err := dial(ctx, "tcp", "example.com:443")
		test.NoError(t, err)
		test.Eq(t, []string{publicAddr + ":443", "93.184.216.35:443"}, asked)
	})

	T.Run("reports every failure when no pinned address connects", func(t *testing.T) {
		t.Parallel()

		dial := PinningDialContext(func(context.Context, string, string) (net.Conn, error) {
			return nil, http.ErrNotSupported
		})

		ctx := withPinnedAddrs(t.Context(), "example.com", []netip.Addr{netip.MustParseAddr(publicAddr)})

		_, err := dial(ctx, "tcp", "example.com:443")
		test.ErrorIs(t, err, http.ErrNotSupported)
	})

	// Refusing rather than falling back to a second resolution: falling back is
	// precisely the lookup this exists to remove.
	T.Run("refuses when nothing pinned suits the network", func(t *testing.T) {
		t.Parallel()

		dial := PinningDialContext(func(context.Context, string, string) (net.Conn, error) {
			t.Error("dialed despite having no dialable pinned address")

			return nil, nil
		})

		ctx := withPinnedAddrs(t.Context(), "example.com", []netip.Addr{netip.MustParseAddr("2606:2800:220:1:248:1893:25c8:1946")})

		_, err := dial(ctx, "tcp4", "example.com:443")
		test.ErrorIs(t, err, ErrNoPinnedAddress)
	})

	// A transport with a proxy configured dials the proxy, not the endpoint.
	// Pinning that connection would send it to the endpoint's address on the
	// proxy's port; the endpoint's own resolution happens at the proxy, out of
	// this dialer's reach either way.
	T.Run("leaves a dial to another host alone", func(t *testing.T) {
		t.Parallel()

		var asked string

		dial := PinningDialContext(func(_ context.Context, _, address string) (net.Conn, error) {
			asked = address

			return nil, http.ErrNotSupported
		})

		ctx := withPinnedAddrs(t.Context(), "example.com", []netip.Addr{netip.MustParseAddr(publicAddr)})

		_, err := dial(ctx, "tcp", "proxy.internal:3128")
		test.Error(t, err)
		test.EqOp(t, "proxy.internal:3128", asked)
	})

	// A replacement PinningURLChecker may hand back an IPv4 address in 4-in-6
	// form, which is not dialable on a "tcp4" network under that spelling.
	T.Run("unmaps a 4-in-6 pinned address", func(t *testing.T) {
		t.Parallel()

		var asked string

		dial := PinningDialContext(func(_ context.Context, _, address string) (net.Conn, error) {
			asked = address

			return nil, http.ErrNotSupported
		})

		ctx := withPinnedAddrs(t.Context(), "example.com",
			[]netip.Addr{netip.AddrFrom16(netip.MustParseAddr(publicAddr).As16())})

		_, err := dial(ctx, "tcp4", "example.com:443")
		test.Error(t, err)
		test.EqOp(t, publicAddr+":443", asked)
	})

	// DNS is case-insensitive, and a transport is under no obligation to hand
	// the dialer the same casing the URL carried.
	T.Run("matches the pinned host regardless of case", func(t *testing.T) {
		t.Parallel()

		var asked string

		dial := PinningDialContext(func(_ context.Context, _, address string) (net.Conn, error) {
			asked = address

			return nil, http.ErrNotSupported
		})

		ctx := withPinnedAddrs(t.Context(), "Example.COM", []netip.Addr{netip.MustParseAddr(publicAddr)})

		_, err := dial(ctx, "tcp", "example.com:443")
		test.Error(t, err)
		test.EqOp(t, publicAddr+":443", asked)
	})

	T.Run("rejects an address it cannot split", func(t *testing.T) {
		t.Parallel()

		dial := PinningDialContext(nil)

		ctx := withPinnedAddrs(t.Context(), "rebind.example.com", []netip.Addr{netip.MustParseAddr(publicAddr)})

		_, err := dial(ctx, "tcp", "example.com")
		test.Error(t, err)
	})
}

func TestDialableOn(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		var (
			v4 = netip.MustParseAddr(publicAddr)
			v6 = netip.MustParseAddr("2606:2800:220:1:248:1893:25c8:1946")
		)

		test.True(t, dialableOn("tcp", v4))
		test.True(t, dialableOn("tcp", v6))

		test.True(t, dialableOn("tcp4", v4))
		test.False(t, dialableOn("tcp4", v6))

		test.False(t, dialableOn("tcp6", v4))
		test.True(t, dialableOn("tcp6", v6))
	})
}

func TestPinningTransport(T *testing.T) {
	T.Parallel()

	T.Run("wraps the dial of an http.Transport", func(t *testing.T) {
		t.Parallel()

		var asked string

		base := &http.Transport{
			DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
				asked = address

				return nil, http.ErrNotSupported
			},
		}

		pinned, ok := pinningTransport(base)
		must.True(t, ok)

		transport, ok := pinned.(*http.Transport)
		must.True(t, ok)

		ctx := withPinnedAddrs(t.Context(), "rebind.example.com", []netip.Addr{netip.MustParseAddr(publicAddr)})

		_, err := transport.DialContext(ctx, "tcp", "rebind.example.com:443")
		test.Error(t, err)
		test.EqOp(t, publicAddr+":443", asked)
	})

	// The transport may be the caller's and used for more than deliveries, so
	// rerouting its dial in place would be a change to something only lent.
	T.Run("leaves the original transport alone", func(t *testing.T) {
		t.Parallel()

		var asked string

		base := &http.Transport{
			DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
				asked = address

				return nil, http.ErrNotSupported
			},
		}

		_, ok := pinningTransport(base)
		must.True(t, ok)

		ctx := withPinnedAddrs(t.Context(), "rebind.example.com", []netip.Addr{netip.MustParseAddr(publicAddr)})

		_, err := base.DialContext(ctx, "tcp", "rebind.example.com:443")
		test.Error(t, err)
		test.EqOp(t, "rebind.example.com:443", asked)
	})

	T.Run("carries the transport's settings across", func(t *testing.T) {
		t.Parallel()

		base := &http.Transport{Proxy: http.ProxyFromEnvironment, MaxIdleConnsPerHost: 7}

		pinned, ok := pinningTransport(base)
		must.True(t, ok)

		transport, ok := pinned.(*http.Transport)
		must.True(t, ok)

		test.EqOp(t, 7, transport.MaxIdleConnsPerHost)
		test.NotNil(t, transport.Proxy)
	})

	T.Run("treats a nil transport as the default one", func(t *testing.T) {
		t.Parallel()

		pinned, ok := pinningTransport(nil)
		must.True(t, ok)

		// Identity rather than equality: DefaultTransport is shared with every
		// other test in the binary and deep-comparing its innards is a race.
		test.False(t, pinned == http.DefaultTransport)
	})

	// A RoundTripper is opaque: there is no way to reach the dialer inside one,
	// and pretending otherwise would report a pin that was never installed.
	T.Run("gives up on a transport it cannot reach into", func(t *testing.T) {
		t.Parallel()

		base := http.RoundTripper(unpinnableTransport{})

		pinned, ok := pinningTransport(base)
		test.False(t, ok)
		test.EqOp(t, base, pinned)
	})
}

// The rebinding case end to end, which is what issue #71 asked for: a resolver
// that answers one way for the check and another for the dial, and a delivery
// that reaches the address the check approved regardless.
func TestWorker_dialPinning(T *testing.T) {
	T.Parallel()

	// httptest's certificate is issued for example.com, so the delivery below
	// completes a real TLS handshake against the *hostname* while connecting to
	// a pinned address — which is the property pinning must not cost.
	newSubscriber := func(t *testing.T) (*httptest.Server, *recordingDialer, *http.Client) {
		t.Helper()

		server := httptest.NewTLSServer(http.HandlerFunc(func(res http.ResponseWriter, _ *http.Request) {
			res.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(server.Close)

		dialer := &recordingDialer{target: server.Listener.Addr().String()}

		client := server.Client()

		transport, ok := client.Transport.(*http.Transport)
		must.True(t, ok)
		transport.DialContext = dialer.dial

		return server, dialer, client
	}

	T.Run("dials the checked address rather than resolving a second time", func(t *testing.T) {
		t.Parallel()

		_, dialer, client := newSubscriber(t)

		resolver := &rebindResolver{}

		w := newTestWorker(t, &fakeStore{},
			WithWorkerPinningURLChecker(NewEndpointURLChecker(resolver)),
			WithHTTPClient(client),
		)

		attempt, err := w.deliver(t.Context(), testDispatch("https://example.com/hooks", 1))
		must.NoError(t, err)
		test.EqOp(t, http.StatusOK, attempt.StatusCode)

		// The address the check approved, not the name it approved it under.
		test.EqOp(t, publicAddr+":443", dialer.asked1(t))

		// And the resolver was consulted exactly once, so the answer it was
		// holding for the dial — the metadata service — never came up.
		test.EqOp(t, int64(1), resolver.calls.Load())
	})

	// The control. A plain URLChecker vets no addresses, so the transport is
	// handed the name and resolves it itself: the gap this issue is about, left
	// open on purpose for the deployments that replaced the policy.
	T.Run("a plain URL checker leaves the dial unpinned", func(t *testing.T) {
		t.Parallel()

		_, dialer, client := newSubscriber(t)

		w := newTestWorker(t, &fakeStore{},
			WithWorkerURLChecker(allowAnyURL),
			WithHTTPClient(client),
		)

		attempt, err := w.deliver(t.Context(), testDispatch("https://example.com/hooks", 1))
		must.NoError(t, err)
		test.EqOp(t, http.StatusOK, attempt.StatusCode)

		test.EqOp(t, "example.com:443", dialer.asked1(t))
	})

	// The rebound answer is refused outright when it is what the check itself
	// sees — the guard that was already here, still doing its job.
	T.Run("refuses an endpoint that has rebound by delivery time", func(t *testing.T) {
		t.Parallel()

		resolver := &rebindResolver{}

		// Burn the one public answer, so the delivery-time check gets the
		// metadata address.
		_, err := NewEndpointURLChecker(resolver)(t.Context(), "https://rebind.example.com/hooks")
		must.NoError(t, err)

		w := newTestWorker(t, &fakeStore{},
			WithWorkerPinningURLChecker(NewEndpointURLChecker(resolver)))

		attempt, err := w.deliver(t.Context(), testDispatch("https://rebind.example.com/hooks", 1))
		must.Error(t, err)
		must.NotNil(t, attempt)
		test.ErrorIs(t, err, ErrDisallowedEndpointHost)
		test.StrContains(t, attempt.Error, metadataAddr)
	})
}
