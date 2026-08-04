package webhooks

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v9/errors"
)

// ErrNoPinnedAddress indicates a dial that was pinned to a set of addresses
// containing nothing it could connect to on the requested network — an
// IPv6-only endpoint on a "tcp4" dial, most plausibly.
//
// It is a refusal rather than a fallback. The alternative is resolving the host
// again, which is the second lookup pinning exists to remove.
var ErrNoPinnedAddress = platformerrors.New("no pinned address is dialable for this webhook delivery")

// DialContextFunc is the shape of net.Dialer's DialContext.
//
// It is an alias rather than a defined type so a dialer written against the raw
// signature — or against httpclient's alias for it — passes without conversion,
// which is what lets PinningDialContext be handed straight to
// httpclient.WithDialWrapper.
type DialContextFunc = func(ctx context.Context, network, address string) (net.Conn, error)

// pinnedAddrsContextKey types the context value carrying a delivery's pin.
type pinnedAddrsContextKey struct{}

// pin is one delivery's destination: the host its URL named, and the addresses
// a URL check approved for that host.
//
// The host is carried because a dialer sees addresses, not requests, and not
// every address a request dials is the request's destination — a transport with
// a proxy configured dials the proxy. Pinning that connection to the endpoint's
// addresses would send it to the endpoint's IP on the proxy's port, so the pin
// applies only when the host being dialed is the host it was taken for.
type pin struct {
	host  string
	addrs []netip.Addr
}

// withPinnedAddrs returns a context whose dials for host, through a dialer
// built by PinningDialContext, connect to addrs and to nothing else. host is
// the URL's hostname, as url.URL.Hostname reports it.
//
// The pin travels on the context rather than in the dialer because one worker
// has one client and one connection pool, and the pinned set is per-delivery: a
// dialer built around a fixed address list would need one transport — and so
// one pool — per endpoint, which is the connection reuse this package exists to
// introduce, undone.
//
// An empty addrs leaves ctx unchanged, so a caller with nothing to pin produces
// an unpinned dial rather than one pinned to nothing.
func withPinnedAddrs(ctx context.Context, host string, addrs []netip.Addr) context.Context {
	if host == "" || len(addrs) == 0 {
		return ctx
	}

	// Cloned because the value outlives this call and the caller's slice is not
	// ours to have aliased.
	return context.WithValue(ctx, pinnedAddrsContextKey{}, pin{host: host, addrs: slices.Clone(addrs)})
}

// pinnedAddrs reports the host a dial on ctx is pinned for and the addresses it
// is pinned to. A nil addrs means ctx pins nothing.
func pinnedAddrs(ctx context.Context) (host string, addrs []netip.Addr) {
	pinned, ok := ctx.Value(pinnedAddrsContextKey{}).(pin)
	if !ok {
		return "", nil
	}

	return pinned.host, pinned.addrs
}

// PinningDialContext wraps a dial function so that a request carrying pinned
// addresses connects to one of them and to nothing else.
//
// This is the half of the SSRF story CheckEndpointURL cannot cover on its own.
// The check resolves a name and approves what it resolved to; the transport
// then resolves it again and connects to whatever comes back the second time.
// An attacker who controls the authoritative server answers those two lookups
// differently — public for the check, 169.254.169.254 for the dial — and every
// guard passes while the worker delivers a signed, authenticated request to the
// metadata service. Pinning removes the second lookup: the address the check
// approved is the address dialed, by construction.
//
// The address the transport asked for is discarded except for its port. What is
// deliberately *not* touched is TLS: the transport handshakes against the
// hostname from the URL, not the address this dialed, so a pinned delivery
// still verifies the subscriber's certificate against the name it registered.
//
// Two dials go through unpinned. One is a context carrying no pin: the same
// client serves things that are not deliveries — a health probe against a
// subscriber, whatever else a caller reaches for EnsureHTTPClient for — and
// refusing those would make the pin a property of the client rather than of the
// delivery. The other is a dial to some host other than the one the pin was
// taken for, which is what a configured proxy produces: the transport connects
// to the proxy and CONNECTs through it, so the endpoint's resolution happens at
// the proxy and is not this dialer's to pin. A deployment that proxies its
// webhook deliveries and needs rebinding closed has to close it at the proxy.
//
// Every delivery a Worker makes carries a pin unless its URL checker was
// replaced with one that vets no addresses; see PinningURLChecker.
//
// A nil base dials with a zero net.Dialer, whose deadline comes from the
// context the request carries.
func PinningDialContext(base DialContextFunc) DialContextFunc {
	if base == nil {
		base = (&net.Dialer{}).DialContext
	}

	return func(ctx context.Context, network, address string) (net.Conn, error) {
		pinnedHost, addrs := pinnedAddrs(ctx)
		if len(addrs) == 0 {
			return base(ctx, network, address)
		}

		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, platformerrors.Wrapf(err, "splitting pinned dial address %q", address)
		}

		// Not the host the pin was taken for, so not the connection it governs:
		// a proxy, most plausibly, whose own address the check never vetted and
		// whose CONNECT does the endpoint's resolution out of this process's
		// reach anyway. Names are compared case-insensitively because DNS is.
		if !strings.EqualFold(host, pinnedHost) {
			return base(ctx, network, address)
		}

		// Tried in order, first connection wins. This is what the resolver's own
		// ordering would have produced, minus the happy-eyeballs racing a
		// net.Dialer does across families — a cost worth paying, since the
		// alternative is handing the address selection back to a resolver that
		// may not answer the same way twice.
		var errs []error

		for _, pinnedAddr := range addrs {
			// Unmapped here as well as at the check, because a replacement
			// PinningURLChecker is under no obligation to have done it and
			// ::ffff:1.2.3.4 is not dialable on a "tcp4" network.
			addr := pinnedAddr.Unmap()

			if !dialableOn(network, addr) {
				continue
			}

			conn, dialErr := base(ctx, network, net.JoinHostPort(addr.String(), port))
			if dialErr == nil {
				return conn, nil
			}

			errs = append(errs, dialErr)
		}

		if len(errs) > 0 {
			return nil, platformerrors.Wrapf(platformerrors.Join(errs...), "dialing pinned addresses for %q", address)
		}

		return nil, platformerrors.Wrapf(ErrNoPinnedAddress, "dialing %q on network %q", address, network)
	}
}

// pinningTransport returns base with its dial routed through
// PinningDialContext, and reports whether it could do so.
//
// It works on an *http.Transport — or on nil, meaning http.DefaultTransport —
// and gives up on anything else, because a RoundTripper is opaque: there is no
// way to reach the dialer inside one, and wrapping it at the RoundTrip level
// cannot influence where the connection underneath goes. A caller whose
// transport is wrapped in instrumentation should install the pin underneath the
// wrapper instead, which is what httpclient.WithDialWrapper is for.
//
// The transport is cloned rather than modified: it may be the caller's, used
// for more than deliveries, and silently rerouting its dial would be a change
// to something this package was only lent. Clone carries the proxy
// configuration, the TLS config, and the pool sizing across; what the clone
// does not share is the pool itself, so a client used both here and elsewhere
// keeps two — which is what the Worker wants anyway, since the point of taking
// a client at all is one warm pool per worker.
func pinningTransport(base http.RoundTripper) (http.RoundTripper, bool) {
	if base == nil {
		base = http.DefaultTransport
	}

	transport, ok := base.(*http.Transport)
	if !ok {
		return base, false
	}

	pinned := transport.Clone()
	pinned.DialContext = PinningDialContext(pinned.DialContext)

	return pinned, true
}

// dialableOn reports whether addr can be connected to on the given network.
//
// An unrecognized network — anything but the family-qualified forms — is
// treated as accepting both, which is what "tcp" means to net.Dialer.
func dialableOn(network string, addr netip.Addr) bool {
	switch network {
	case "tcp4", "udp4", "ip4":
		return addr.Is4()
	case "tcp6", "udp6", "ip6":
		return addr.Is6() && !addr.Is4In6()
	default:
		return true
	}
}
