package requestsigning

import (
	"context"
	"net/http"
	"slices"
	"testing"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// stubVerifier stands in for a provider scheme registered from elsewhere.
type stubVerifier struct{ scheme string }

var _ Verifier = (*stubVerifier)(nil)

func (s *stubVerifier) Scheme() string { return s.scheme }

func (*stubVerifier) VerifyRequest(context.Context, http.Header, []byte) error { return nil }

// Not parallel, and neither are its subtests: the registry is package-level
// state, and a test registering a name while another reads the list is a flake
// nobody would enjoy diagnosing.
//
//nolint:paralleltest // the scheme registry is package-level state
func TestRegisterScheme(T *testing.T) {
	T.Run("standard", func(t *testing.T) {
		factory := func(KeySource, ...Option) (Verifier, error) { return &stubVerifier{scheme: "acme"}, nil }

		must.NoError(t, RegisterScheme("acme", factory))
		t.Cleanup(func() { unregisterScheme("acme") })

		verifier, err := NewVerifierForScheme("acme", StaticKeyring(Keyring{Current: []byte("k")}))
		must.NoError(t, err)
		test.EqOp(t, "acme", verifier.Scheme())

		test.True(t, slices.Contains(RegisteredSchemes(), "acme"))
	})

	// Import order must not decide which code verifies this service's inbound
	// requests.
	T.Run("refuses to overwrite a registered scheme", func(t *testing.T) {
		factory := func(KeySource, ...Option) (Verifier, error) { return &stubVerifier{scheme: "acme"}, nil }

		must.NoError(t, RegisterScheme("dup", factory))
		t.Cleanup(func() { unregisterScheme("dup") })

		test.ErrorIs(t, RegisterScheme("dup", factory), ErrSchemeRegistered)
	})

	T.Run("v1 is registered by this package", func(t *testing.T) {
		verifier, err := NewVerifierForScheme(SchemeV1, StaticKeyring(Keyring{Current: []byte("k")}))
		must.NoError(t, err)

		test.EqOp(t, SchemeV1, verifier.Scheme())
		test.True(t, slices.Contains(RegisteredSchemes(), SchemeV1))
	})

	// An unrecognized scheme is a startup failure, never a permissive default:
	// a noop verifier is not a degraded mode, it is an open door.
	T.Run("an unregistered scheme is an unknown provider", func(t *testing.T) {
		_, err := NewVerifierForScheme("nope", StaticKeyring(Keyring{Current: []byte("k")}))
		test.ErrorIs(t, err, platformerrors.ErrUnknownProvider)
	})

	T.Run("rejects its own bad inputs", func(t *testing.T) {
		test.ErrorIs(t, RegisterScheme("", func(KeySource, ...Option) (Verifier, error) { return nil, nil }),
			platformerrors.ErrEmptyInputParameter)

		test.ErrorIs(t, RegisterScheme("nilfactory", nil), platformerrors.ErrNilInputParameter)
	})
}

// unregisterScheme drops a scheme, so a test that registered one leaves the
// registry as it found it. There is deliberately no exported counterpart:
// unregistering in a running process would mean an endpoint's verifier could
// disappear underneath it.
func unregisterScheme(scheme string) {
	schemesMu.Lock()
	defer schemesMu.Unlock()

	delete(schemes, scheme)
}
