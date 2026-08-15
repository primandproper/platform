package webauthn

import (
	"context"
	"strings"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-webauthn/webauthn/protocol"
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
)

// DefaultCeremonyTimeout is how long a ceremony may take when nothing says
// otherwise: long enough for a user to find the authenticator, plug it in, and
// touch it, and short enough that an abandoned ceremony's state is gone before
// anybody thinks about it again.
//
// It is one minute because that is what the WebAuthn specification suggests for
// a ceremony where user verification is expected, and because the value has to
// come from somewhere — a deployment that knows better sets it.
const DefaultCeremonyTimeout = time.Minute

// User verification requirements, as configured. They are the protocol's own
// values spelled as strings, because a Config comes out of the environment.
const (
	// UserVerificationRequired refuses a ceremony the authenticator did not
	// verify the user for — a PIN, a fingerprint, a face. This is what makes a
	// passkey a second factor as well as a first.
	UserVerificationRequired = string(protocol.VerificationRequired)
	// UserVerificationPreferred asks for verification and accepts a ceremony
	// without it. It is the protocol's default and this package's.
	UserVerificationPreferred = string(protocol.VerificationPreferred)
	// UserVerificationDiscouraged asks the authenticator not to verify, for a
	// deployment where the passkey is one factor among others.
	UserVerificationDiscouraged = string(protocol.VerificationDiscouraged)
)

// userVerifications are the values UserVerification accepts. The empty string
// is one of them and means the default.
var userVerifications = []any{
	"",
	UserVerificationRequired,
	UserVerificationPreferred,
	UserVerificationDiscouraged,
}

// Config is the Relying Party — the server half of a WebAuthn ceremony — as a
// deployment configures it.
//
// It is deliberately smaller than go-webauthn's own configuration. What is here
// is what changes between deployments of the same application; the rest of the
// protocol's knobs are per-ceremony and are passed to BeginRegistration and
// BeginLogin as [RegistrationOption] and [LoginOption], which are the library's
// own. A deployment that needs what neither covers — FIDO metadata-service
// attestation validation, AAGUID filtering — builds go-webauthn itself and
// keeps [SessionStore], which is the half worth having and is usable on its
// own.
type Config struct {
	_ struct{} `json:"-" yaml:"-"`

	// RPID is the Relying Party identifier: the site's effective domain, with
	// no scheme and no port. "example.com" covers app.example.com; "localhost"
	// is what a development deployment uses.
	//
	// It is the scope of every credential registered under it. Changing it
	// invalidates every passkey a deployment has issued, because an
	// authenticator will not answer for a domain it did not register against.
	RPID string `env:"ID" json:"rpID,omitempty" yaml:"rpID,omitempty"`

	// RPDisplayName is the human-readable name the authenticator shows during
	// registration — "Example", not "example.com". The library requires one.
	RPDisplayName string `env:"DISPLAY_NAME" json:"rpDisplayName,omitempty" yaml:"rpDisplayName,omitempty"`

	// UserVerification is the deployment's user-verification policy:
	// required, preferred, or discouraged. Empty means preferred, which is the
	// protocol's default. A single ceremony may override it through a
	// [LoginOption] or a [RegistrationOption].
	UserVerification string `env:"USER_VERIFICATION" json:"userVerification,omitempty" yaml:"userVerification,omitempty"`

	// RPOrigins is every origin a ceremony may be answered from, fully
	// qualified — "https://example.com", including the port when there is a
	// non-default one. At least one is required, and an origin that is missing
	// here is a login that fails verification rather than one that is merely
	// unstyled.
	RPOrigins []string `env:"ORIGINS" json:"rpOrigins,omitempty" yaml:"rpOrigins,omitempty"`

	// CeremonyTimeout bounds how long a ceremony may take. Zero means
	// DefaultCeremonyTimeout.
	//
	// It is one number in three places, which is the point of it being one
	// field: it is the timeout the browser is asked to honor, the expiry the
	// library enforces when it verifies the response, and the TTL the ceremony
	// state is stored under. Configured separately, those three drift, and the
	// symptom is a ceremony that the client abandons while the server still
	// holds a challenge it will honor.
	CeremonyTimeout time.Duration `env:"CEREMONY_TIMEOUT" json:"ceremonyTimeout,omitempty" yaml:"ceremonyTimeout,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults fills in zero fields.
func (cfg *Config) EnsureDefaults() {
	if cfg.CeremonyTimeout == 0 {
		cfg.CeremonyTimeout = DefaultCeremonyTimeout
	}
}

// ValidateWithContext validates a Config struct.
//
// The three required fields are required by the protocol rather than by taste:
// go-webauthn refuses to begin a ceremony without an RPID, a display name, or
// an origin. Checking them here turns "the first passkey registration of the
// day failed" into a service that did not start.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.RPID, validation.Required, validation.By(func(any) error {
			// Vetted as a domain by the library's own rule rather than by a
			// pattern here, since it is the library that will refuse it.
			return protocol.ValidateRPID(cfg.RPID)
		})),
		validation.Field(&cfg.RPDisplayName, validation.Required),
		validation.Field(&cfg.RPOrigins, validation.Required, validation.Length(1, 0)),
		validation.Field(&cfg.UserVerification, validation.In(userVerifications...)),
		validation.Field(&cfg.CeremonyTimeout, validation.Min(time.Duration(0))),
	)
}

// userVerification normalizes the configured requirement, so that trailing
// whitespace out of an environment file is not a different policy.
func (cfg *Config) userVerification() protocol.UserVerificationRequirement {
	return protocol.UserVerificationRequirement(strings.TrimSpace(strings.ToLower(cfg.UserVerification)))
}

// protocolConfig renders the Config as go-webauthn's own.
//
// Enforce is on for both ceremonies, deliberately and not configurably. It is
// what makes the library stamp SessionData.Expires and check it when the
// response comes back, so the deadline is enforced by the server rather than
// requested of the browser — a client that ignores the timeout it was given is
// exactly the client the deadline is for.
func (cfg *Config) protocolConfig() *gowebauthn.Config {
	timeout := gowebauthn.TimeoutConfig{
		Enforce:    true,
		Timeout:    cfg.CeremonyTimeout,
		TimeoutUVD: cfg.CeremonyTimeout,
	}

	return &gowebauthn.Config{
		RPID:          cfg.RPID,
		RPDisplayName: cfg.RPDisplayName,
		RPOrigins:     cfg.RPOrigins,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			UserVerification: cfg.userVerification(),
		},
		Timeouts: gowebauthn.TimeoutsConfig{
			Login:        timeout,
			Registration: timeout,
		},
	}
}
