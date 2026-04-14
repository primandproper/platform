package authentication

import (
	"context"
)

type (
	// Hasher hashes passwords.
	Hasher interface {
		HashPassword(ctx context.Context, password string) (string, error)
	}

	// Authenticator hashes passwords and verifies them against a stored hash.
	//
	// Second-factor verification (TOTP, WebAuthn, backup codes, etc.) is
	// intentionally NOT part of this interface. Callers compose password
	// verification with any second-factor verifier they need — see the
	// authentication/totp package for the TOTP verifier.
	Authenticator interface {
		Hasher

		// PasswordMatches reports whether password matches hash. A non-match
		// returns (false, nil); only genuine errors (malformed hash, runtime
		// failure) populate err.
		PasswordMatches(ctx context.Context, hash, password string) (bool, error)
	}
)
