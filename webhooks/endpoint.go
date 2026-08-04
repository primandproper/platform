package webhooks

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v9/errors"
)

// DefaultContentType is the Content-Type deliveries carry when an Endpoint does
// not set one.
const DefaultContentType = "application/json"

var (
	// ErrInvalidEndpointURL indicates a URL that is unparseable, not absolute,
	// or not https.
	ErrInvalidEndpointURL = platformerrors.New("invalid webhook endpoint URL")

	// ErrDisallowedEndpointHost indicates a URL whose host resolves somewhere a
	// webhook must not reach — loopback, link-local, or private address space.
	// See CheckEndpointURL for why this is enforced at registration.
	ErrDisallowedEndpointHost = platformerrors.New("webhook endpoint host is not publicly routable")

	// ErrReservedHeader indicates an Endpoint whose static headers would
	// overwrite one this package sets.
	ErrReservedHeader = platformerrors.New("webhook endpoint sets a reserved header")

	// ErrNoEvents indicates an endpoint subscribing to nothing, which is never
	// what the registrant meant.
	ErrNoEvents = platformerrors.New("webhook endpoint subscribes to no events")
)

// reservedHeaders are the headers this package sets on every request. An
// Endpoint's static Headers may not contain them: a subscriber that could
// overwrite its own signature header would be authenticating deliveries against
// a value it chose.
var reservedHeaders = []string{
	SignatureHeader,
	TimestampHeader,
	EventTypeHeader,
	DeliveryIDHeader,
	AttemptHeader,
	"Content-Type",
}

// Resolver is the name-resolution seam the endpoint check runs through.
// *net.Resolver satisfies it, and net.DefaultResolver is what the package-level
// checks use.
//
// It is an interface for one reason: DNS rebinding is an attack made entirely
// of a resolver answering one way and then another, and it cannot be staged
// against real DNS from a test. A resolver that returns a public address on the
// first lookup and a private one on the second is the whole attack, and it is
// three lines to write against this.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// CheckEndpointURL reports whether rawURL is acceptable as a delivery target.
//
// This is SSRF prevention, and it is worth being explicit about what it does
// and does not buy. A webhook endpoint is a URL supplied by a user that the
// server will then make authenticated requests to, which is the textbook shape
// of a server-side request forgery: point it at 169.254.169.254 and the
// delivery worker fetches cloud instance credentials on the attacker's behalf,
// or point it at an internal admin service and the worker reaches something the
// attacker cannot.
//
// So: https only, and no host that resolves into loopback, link-local, private,
// or otherwise non-global address space.
//
// The check runs at registration, where a rejection can be reported to whoever
// submitted the URL, and again at delivery, because registration alone is not
// sound: DNS is mutable, and a name that resolved publicly when it was
// registered can resolve to 127.0.0.1 by the time the worker dials it.
//
// What this alone does not close is DNS rebinding: resolution and connection
// are separate steps, and an attacker controlling the authoritative server can
// answer this lookup with a public address and the dial that follows with a
// private one. Closing it means dialing the addresses this check accepted
// rather than resolving a second time, which is what CheckEndpointURLAddrs and
// PinningDialContext are for and what the Worker does by default. Use this
// function where a verdict is all that is wanted — registration, an admin
// form's validation — and CheckEndpointURLAddrs anywhere the result is about to
// be connected to.
func CheckEndpointURL(ctx context.Context, rawURL string) error {
	_, err := CheckEndpointURLAddrs(ctx, rawURL)

	return err
}

// CheckEndpointURLAddrs is CheckEndpointURL, returning the addresses it
// validated so they can be pinned into the dial.
//
// The returned addresses are the entire acceptable destination set for rawURL
// as of this call: every one of them passed the same routability check, and no
// address outside the set did. Handing them to PinningDialContext — via
// WithPinnedAddrs on the request's context — is what removes the window between
// deciding a host is safe and connecting to it.
func CheckEndpointURLAddrs(ctx context.Context, rawURL string) ([]netip.Addr, error) {
	return checkEndpointURL(ctx, rawURL, net.DefaultResolver)
}

// NewEndpointURLChecker returns CheckEndpointURLAddrs resolving through r
// instead of net.DefaultResolver. A nil r means net.DefaultResolver.
//
// The resolver is worth overriding for a deployment that resolves subscriber
// names through something other than the host's configured servers — a resolver
// pinned to a particular upstream, or one with its own cache. It must be the
// same resolution the dial would have performed, or pinning trades a rebinding
// window for a name that is simply resolved wrongly.
func NewEndpointURLChecker(r Resolver) PinningURLChecker {
	if r == nil {
		r = net.DefaultResolver
	}

	return func(ctx context.Context, rawURL string) ([]netip.Addr, error) {
		return checkEndpointURL(ctx, rawURL, r)
	}
}

// checkEndpointURL is the body both exported checks share.
func checkEndpointURL(ctx context.Context, rawURL string, resolver Resolver) ([]netip.Addr, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, platformerrors.Wrapf(ErrInvalidEndpointURL, "parsing %q: %v", rawURL, err)
	}

	// https only. Plaintext delivery would put the payload — and the headers
	// that authenticate it — on the wire in clear, and a signature does not make
	// a payload confidential.
	if parsed.Scheme != "https" {
		return nil, platformerrors.Wrapf(ErrInvalidEndpointURL, "scheme %q is not https", parsed.Scheme)
	}

	if parsed.Host == "" {
		return nil, platformerrors.Wrapf(ErrInvalidEndpointURL, "no host in %q", rawURL)
	}

	if parsed.User != nil {
		// Credentials in the URL would be logged everywhere the URL is, and the
		// signature is how a subscriber authenticates us — not basic auth.
		return nil, platformerrors.Wrapf(ErrInvalidEndpointURL, "userinfo is not permitted in an endpoint URL")
	}

	host := parsed.Hostname()

	// A literal IP is checked directly; a name is checked against everything it
	// currently resolves to.
	if ip := net.ParseIP(host); ip != nil {
		addr, addrErr := checkAddr(ip, host)
		if addrErr != nil {
			return nil, addrErr
		}

		return []netip.Addr{addr}, nil
	}

	// Resolved through the context-aware resolver rather than net.LookupIP: this
	// runs on the delivery path, and a name whose authoritative server is
	// blackholed would otherwise hang a worker goroutine for the resolver's own
	// timeout with nothing able to cancel it.
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, platformerrors.Wrapf(ErrInvalidEndpointURL, "resolving %q: %v", host, err)
	}

	if len(ips) == 0 {
		return nil, platformerrors.Wrapf(ErrInvalidEndpointURL, "%q resolves to no addresses", host)
	}

	// Every resolved address must be acceptable, not merely one of them. A name
	// that returns both a public and a loopback address would otherwise pass
	// here and then be dialed at whichever the resolver returned first.
	addrs := make([]netip.Addr, 0, len(ips))

	for i := range ips {
		addr, addrErr := checkAddr(ips[i].IP, host)
		if addrErr != nil {
			return nil, addrErr
		}

		addrs = append(addrs, addr)
	}

	return addrs, nil
}

// checkAddr vets one address and returns it in the form the dialer pins on.
//
// The conversion is a check in its own right, not a formality: a Resolver is an
// interface, and an implementation that hands back something neither 4 nor 16
// bytes long has named an address nothing can dial. Refusing it here is what
// keeps an unusable address from either being pinned or, worse, quietly
// dropped — a set with one address silently removed is a set the dial can no
// longer be held to.
//
// Unmapped, so that an IPv4 address that arrived in 4-in-6 form is dialed as
// 1.2.3.4 rather than ::ffff:1.2.3.4: the two name the same host, but only one
// of them is dialable on a "tcp4" network.
func checkAddr(ip net.IP, host string) (netip.Addr, error) {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}, platformerrors.Wrapf(ErrDisallowedEndpointHost, "%q resolves to an unusable address", host)
	}

	if err := checkIP(ip, host); err != nil {
		return netip.Addr{}, err
	}

	return addr.Unmap(), nil
}

// checkIP rejects an address that is not globally routable.
//
// IsGlobalUnicast is necessary but not sufficient: it admits the RFC 1918
// private ranges and unique-local IPv6, which are precisely the internal
// networks this is meant to keep deliveries out of.
func checkIP(ip net.IP, host string) error {
	switch {
	case ip.IsLoopback():
		return platformerrors.Wrapf(ErrDisallowedEndpointHost, "%q resolves to loopback address %s", host, ip)
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		// 169.254.0.0/16 — the cloud instance metadata range.
		return platformerrors.Wrapf(ErrDisallowedEndpointHost, "%q resolves to link-local address %s", host, ip)
	case ip.IsPrivate():
		return platformerrors.Wrapf(ErrDisallowedEndpointHost, "%q resolves to private address %s", host, ip)
	case ip.IsUnspecified():
		return platformerrors.Wrapf(ErrDisallowedEndpointHost, "%q resolves to unspecified address %s", host, ip)
	case ip.IsMulticast(), ip.IsInterfaceLocalMulticast():
		return platformerrors.Wrapf(ErrDisallowedEndpointHost, "%q resolves to multicast address %s", host, ip)
	case !ip.IsGlobalUnicast():
		return platformerrors.Wrapf(ErrDisallowedEndpointHost, "%q resolves to non-global address %s", host, ip)
	default:
		return nil
	}
}

// URLChecker vets a delivery target. CheckEndpointURL is the implementation
// this package uses unless a caller replaces it.
//
// It is replaceable because a minority of deployments deliver webhooks to
// internal services on purpose — a sidecar, another service on the same private
// network — and for them CheckEndpointURL's refusal is not a safety property
// but a wall. Replacing it means owning the SSRF question yourself: the
// replacement is the only thing standing between a user-supplied URL and an
// authenticated request from inside your network, so it should be an allowlist
// of hosts you operate, not a function that returns nil.
//
// A checker of this shape reports a verdict and nothing else, so a Worker using
// one has nothing to pin its dial to and does not pin it. That is deliberate:
// pinning a dial to addresses the deployment's own checker never vetted would
// enforce a policy nobody asked for. Deployments that want both write a
// PinningURLChecker instead.
type URLChecker func(ctx context.Context, rawURL string) error

// PinningURLChecker vets a delivery target and reports the addresses it
// accepted, so the delivery that follows can connect to those and to nothing
// else. CheckEndpointURLAddrs is the implementation the Worker uses unless a
// caller replaces it.
//
// This is the shape that closes DNS rebinding, and the reason it is a distinct
// type from URLChecker rather than a wider one: the addresses are a claim about
// what this checker was willing to permit, and only a checker that made that
// claim can have it enforced. Returning no addresses and no error is legal and
// means "acceptable, but do not pin" — the escape hatch for a checker that
// approves by name and has no opinion about where the name points.
type PinningURLChecker func(ctx context.Context, rawURL string) ([]netip.Addr, error)

// EnsureDefaults fills an Endpoint's optional fields.
func (e *Endpoint) EnsureDefaults() {
	if e == nil {
		return
	}

	if e.ContentType == "" {
		e.ContentType = DefaultContentType
	}
}

// Validate checks an Endpoint against the catalog it is being registered into
// and the URL policy it will be delivered under.
//
// The catalog is an argument because an endpoint is only meaningful relative to
// the set of events an application publishes: a subscription to an event that
// does not exist is a silent no-op forever. checkURL is an argument so that
// registration and delivery cannot apply different policies — an endpoint
// accepted here and refused by the worker would sit in the backlog until it
// died. A nil checkURL means CheckEndpointURL.
func (e *Endpoint) Validate(ctx context.Context, catalog Catalog, checkURL URLChecker) error {
	if e == nil {
		return ErrNilEndpoint
	}

	if checkURL == nil {
		checkURL = CheckEndpointURL
	}

	if err := checkURL(ctx, e.URL); err != nil {
		return err
	}

	if len(e.Secret.Current) == 0 {
		return ErrNoSigningSecret
	}

	if len(e.Events) == 0 {
		return ErrNoEvents
	}

	for _, event := range e.Events {
		if !catalog.Known(event) {
			return platformerrors.Wrapf(ErrUnknownEventType, "event type %q", event)
		}
	}

	for name := range e.Headers {
		if slices.ContainsFunc(reservedHeaders, func(reserved string) bool {
			return strings.EqualFold(name, reserved)
		}) {
			return platformerrors.Wrapf(ErrReservedHeader, "header %q", name)
		}
	}

	return nil
}

// applyHeaders writes the endpoint's static headers onto a request. Reserved
// headers are rejected at registration, so nothing here can overwrite one; the
// check is repeated defensively because an Endpoint can also arrive from a Store
// implementation this package did not validate.
func (e *Endpoint) applyHeaders(header http.Header) {
	for name, value := range e.Headers {
		if slices.ContainsFunc(reservedHeaders, func(reserved string) bool {
			return strings.EqualFold(name, reserved)
		}) {
			continue
		}

		header.Set(name, value)
	}
}
