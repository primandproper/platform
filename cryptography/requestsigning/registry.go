package requestsigning

import (
	"slices"
	"sync"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

// VerifierFactory builds a Verifier for one scheme from the keys it should
// trust. It is what a scheme registers, rather than a Verifier, because a
// verifier holds key material and an init-time registration has none.
type VerifierFactory func(keys KeySource, opts ...Option) (Verifier, error)

// ErrSchemeRegistered indicates a second registration under a scheme name that
// is already taken. Registration is rejected rather than allowed to overwrite:
// the last package to run its init would otherwise decide which code verifies
// this service's inbound requests, which is not a property that should depend
// on import order.
var ErrSchemeRegistered = platformerrors.New("signature scheme already registered")

var (
	schemesMu sync.RWMutex
	schemes   = map[string]VerifierFactory{
		SchemeV1: NewVerifier,
	}
)

// RegisterScheme records factory as the way to build a verifier for scheme.
//
// It is how the per-provider inbound schemes — Stripe's t=…,v1=…, GitHub's
// X-Hub-Signature-256, a partner's homegrown one — become selectable by name
// without this package importing any of them, and without every service
// growing a switch. A provider package calls it from init; the service names
// the scheme in config and gets the verifier through NewVerifierForScheme.
//
// SchemeV1 is registered by this package. Registering the same name twice
// returns ErrSchemeRegistered.
func RegisterScheme(scheme string, factory VerifierFactory) error {
	if scheme == "" {
		return platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "signature scheme name")
	}

	if factory == nil {
		return platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil verifier factory")
	}

	schemesMu.Lock()
	defer schemesMu.Unlock()

	if _, exists := schemes[scheme]; exists {
		return platformerrors.Wrapf(ErrSchemeRegistered, "scheme %q", scheme)
	}

	schemes[scheme] = factory

	return nil
}

// NewVerifierForScheme builds the verifier registered under scheme.
//
// An unregistered scheme returns errors.ErrUnknownProvider rather than a
// verifier that accepts everything. A noop here is not a degraded mode, it is
// an open door, so a service whose config names a scheme this build does not
// carry fails to start instead of serving unauthenticated traffic that looks
// authenticated.
//
// This is a wiring-time lookup, not a per-request one. Sniffing the scheme out
// of an incoming request would let the caller choose which verifier judges it,
// and a caller who can choose picks the weakest one on offer.
func NewVerifierForScheme(scheme string, keys KeySource, opts ...Option) (Verifier, error) {
	schemesMu.RLock()
	factory, ok := schemes[scheme]
	schemesMu.RUnlock()

	if !ok {
		return nil, platformerrors.Wrapf(platformerrors.ErrUnknownProvider, "signature scheme %q", scheme)
	}

	return factory(keys, opts...)
}

// RegisteredSchemes returns the registered scheme names, sorted. It is for
// rendering a config error that says what this build actually supports.
func RegisteredSchemes() []string {
	schemesMu.RLock()
	defer schemesMu.RUnlock()

	names := make([]string, 0, len(schemes))
	for name := range schemes {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}
