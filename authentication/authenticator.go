package authentication

import (
	"context"

	platformerrors "github.com/primandproper/platform/errors"
)

var (
	// ErrInvalidTOTPToken indicates that a provided two-factor code is invalid.
	ErrInvalidTOTPToken = platformerrors.New("invalid two factor code")
	// ErrTOTPRequired indicates that the user has TOTP enabled but did not provide a code.
	ErrTOTPRequired = platformerrors.New("TOTP code required but not provided")
	// ErrPasswordDoesNotMatch indicates that a provided passwords does not match.
	ErrPasswordDoesNotMatch = platformerrors.New("password does not match")
)

type (
	// Hasher hashes passwords.
	Hasher interface {
		HashPassword(ctx context.Context, password string) (string, error)
	}

	// Authenticator authenticates users.
	Authenticator interface {
		Hasher

		CredentialsAreValid(ctx context.Context, hash, password, totpSecret, totpCode string) (bool, error)
	}
)
