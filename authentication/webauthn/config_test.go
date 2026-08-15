package webauthn

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("supplies a ceremony timeout", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		test.EqOp(t, DefaultCeremonyTimeout, cfg.CeremonyTimeout)
	})

	T.Run("leaves a configured timeout alone", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{CeremonyTimeout: 30 * time.Second}
		cfg.EnsureDefaults()

		test.EqOp(t, 30*time.Second, cfg.CeremonyTimeout)
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	valid := func() *Config {
		return &Config{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		}
	}

	T.Run("accepts the three fields a ceremony needs", func(t *testing.T) {
		t.Parallel()

		must.NoError(t, valid().ValidateWithContext(t.Context()))
	})

	T.Run("accepts every user verification policy, and none", func(t *testing.T) {
		t.Parallel()

		for _, uv := range []string{
			"", UserVerificationRequired, UserVerificationPreferred, UserVerificationDiscouraged,
		} {
			cfg := valid()
			cfg.UserVerification = uv

			test.NoError(t, cfg.ValidateWithContext(t.Context()), test.Sprintf("policy %q", uv))
		}
	})

	// Every one of these is a service that starts and then cannot register a
	// passkey, which is a failure a user finds rather than a deployment.
	T.Run("rejects what the library would refuse at the first ceremony", func(t *testing.T) {
		t.Parallel()

		for name, mutate := range map[string]func(*Config){
			"no relying party id": func(cfg *Config) { cfg.RPID = "" },
			"no display name":     func(cfg *Config) { cfg.RPDisplayName = "" },
			"no origins":          func(cfg *Config) { cfg.RPOrigins = nil },
			// An origin is not an identifier: the identifier is the effective
			// domain, with no scheme and no port.
			"a relying party id with a scheme": func(cfg *Config) { cfg.RPID = "https://example.com" },
			"an unknown verification policy":   func(cfg *Config) { cfg.UserVerification = "sometimes" },
			"a negative ceremony timeout":      func(cfg *Config) { cfg.CeremonyTimeout = -time.Second },
		} {
			cfg := valid()
			mutate(cfg)

			test.Error(t, cfg.ValidateWithContext(t.Context()), test.Sprintf("config %q", name))
		}
	})
}

func TestConfig_protocolConfig(T *testing.T) {
	T.Parallel()

	T.Run("carries the configured relying party across", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			RPID:            "example.com",
			RPDisplayName:   "Example",
			RPOrigins:       []string{"https://example.com"},
			CeremonyTimeout: 42 * time.Second,
		}

		protocolCfg := cfg.protocolConfig()

		test.EqOp(t, "example.com", protocolCfg.RPID)
		test.EqOp(t, "Example", protocolCfg.RPDisplayName)
		test.Eq(t, []string{"https://example.com"}, protocolCfg.RPOrigins)
	})

	// The one number in three places. Both ceremonies get the same bound, and
	// both get it whether or not user verification is discouraged — a ceremony
	// that takes longer because the authenticator asked for a PIN is not a
	// different ceremony.
	T.Run("gives both ceremonies the same enforced deadline", func(t *testing.T) {
		t.Parallel()

		protocolCfg := (&Config{CeremonyTimeout: 42 * time.Second}).protocolConfig()

		test.True(t, protocolCfg.Timeouts.Login.Enforce)
		test.True(t, protocolCfg.Timeouts.Registration.Enforce)
		test.EqOp(t, 42*time.Second, protocolCfg.Timeouts.Login.Timeout)
		test.EqOp(t, 42*time.Second, protocolCfg.Timeouts.Login.TimeoutUVD)
		test.EqOp(t, 42*time.Second, protocolCfg.Timeouts.Registration.Timeout)
		test.EqOp(t, 42*time.Second, protocolCfg.Timeouts.Registration.TimeoutUVD)
	})

	// Normalized, so that a trailing newline out of an environment file is not
	// a policy the library has never heard of — which it would answer by
	// verifying nothing.
	T.Run("normalizes the verification policy", func(t *testing.T) {
		t.Parallel()

		for _, spelling := range []string{"required", " Required ", "REQUIRED\n"} {
			protocolCfg := (&Config{UserVerification: spelling}).protocolConfig()

			test.EqOp(t, protocol.VerificationRequired,
				protocolCfg.AuthenticatorSelection.UserVerification, test.Sprintf("spelling %q", spelling))
		}
	})

	T.Run("leaves the policy unset when none is configured", func(t *testing.T) {
		t.Parallel()

		protocolCfg := (&Config{}).protocolConfig()

		// Unset rather than defaulted here: the protocol's own default is
		// preferred, and spelling it out would mean this package deciding what
		// the specification already decides.
		test.EqOp(t, protocol.UserVerificationRequirement(""),
			protocolCfg.AuthenticatorSelection.UserVerification)
	})
}
