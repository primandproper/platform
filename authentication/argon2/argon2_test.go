package argon2_test

import (
	"testing"
	"time"

	"github.com/primandproper/platform/authentication/argon2"
	loggingnoop "github.com/primandproper/platform/observability/logging/noop"
	tracingnoop "github.com/primandproper/platform/observability/tracing/noop"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
)

const (
	examplePassword        = "Pa$$w0rdPa$$w0rdPa$$w0rdPa$$w0rd"
	exampleTwoFactorSecret = "HEREISASECRETWHICHIVEMADEUPBECAUSEIWANNATESTRELIABLY"

	argon2HashedExamplePassword = `$argon2id$v=19$m=65536,t=1,p=2$C+YWiNi21e94acF3ip8UGA$Ru6oL96HZSP7cVcfAbRwOuK9+vwBo/BLhCzOrGrMH0M`
)

func TestArgon2_HashPassword(T *testing.T) {
	T.Parallel()

	x := argon2.ProvideArgon2Authenticator(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider())

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		actual, err := x.HashPassword(ctx, examplePassword)
		assert.NoError(t, err)
		assert.NotEmpty(t, actual)
	})
}

func TestArgon2_ValidateLogin(T *testing.T) {
	T.Parallel()

	x := argon2.ProvideArgon2Authenticator(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider())

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		code, err := totp.GenerateCode(exampleTwoFactorSecret, time.Now().UTC())
		assert.NoError(t, err, "error generating code to validate login")

		valid, err := x.CredentialsAreValid(
			ctx,
			argon2HashedExamplePassword,
			examplePassword,
			exampleTwoFactorSecret,
			code,
		)
		assert.NoError(t, err, "unexpected error encountered validating login: %v", err)
		assert.True(t, valid)
	})

	T.Run("without two factor secret", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		valid, err := x.CredentialsAreValid(
			ctx,
			argon2HashedExamplePassword,
			examplePassword,
			"",
			"",
		)
		assert.NoError(t, err, "unexpected error encountered validating login: %v", err)
		assert.True(t, valid)
	})

	T.Run("with error determining if password matches", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		valid, err := x.CredentialsAreValid(
			ctx,
			"       blah blah blah not a valid hash lol           ",
			examplePassword,
			"",
			"",
		)
		assert.Error(t, err, "unexpected error encountered validating login: %v", err)
		assert.False(t, valid)
	})

	T.Run("with non-matching password", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		code, err := totp.GenerateCode(exampleTwoFactorSecret, time.Now().UTC())
		assert.NoError(t, err, "error generating code to validate login")

		valid, err := x.CredentialsAreValid(
			ctx,
			argon2HashedExamplePassword,
			"examplePassword",
			exampleTwoFactorSecret,
			code,
		)
		assert.Error(t, err, "unexpected error encountered validating login: %v", err)
		assert.False(t, valid)
	})

	T.Run("with invalid code", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		valid, err := x.CredentialsAreValid(
			ctx,
			argon2HashedExamplePassword,
			examplePassword,
			exampleTwoFactorSecret,
			"CODE",
		)
		assert.Error(t, err, "unexpected error encountered validating login: %v", err)
		assert.True(t, valid)
	})
}

func TestProvideArgon2Authenticator(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		argon2.ProvideArgon2Authenticator(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider())
	})
}
