package argon2

import (
	"context"
	"crypto/rand"
	"math"
	"runtime"

	"github.com/primandproper/platform/authentication"
	"github.com/primandproper/platform/observability"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/alexedwards/argon2id"
	"github.com/pquerna/otp/totp"
)

func init() {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
}

const (
	serviceName        = "argon2"
	sixtyFourMegabytes = 2<<15 - 1
)

var (
	argonParams = &argon2id.Params{
		Memory:      sixtyFourMegabytes,
		Iterations:  1,
		Parallelism: uint8(math.Max(2, float64(runtime.NumCPU()))),
		SaltLength:  16,
		KeyLength:   32,
	}
)

type (
	// Argon2Authenticator is our argon2-based authenticator.
	Argon2Authenticator struct {
		logger logging.Logger
		tracer tracing.Tracer
	}
)

// ProvideArgon2Authenticator returns an argon2 powered Argon2Authenticator.
func ProvideArgon2Authenticator(logger logging.Logger, tracerProvider tracing.TracerProvider) authentication.Authenticator {
	ba := &Argon2Authenticator{
		logger: logging.NewNamedLogger(logger, serviceName),
		tracer: tracing.NewNamedTracer(tracerProvider, serviceName),
	}

	return ba
}

// HashPassword takes a password and hashes it using argon2.
func (a *Argon2Authenticator) HashPassword(ctx context.Context, password string) (string, error) {
	_, span := a.tracer.StartSpan(ctx)
	defer span.End()

	return argon2id.CreateHash(password, argonParams)
}

// CredentialsAreValid validates a login attempt by:
//   - checking that the provided authentication matches the provided hashed passwords.
//   - checking that the temporary one-time authentication provided jives with the provided two factor secret.
func (a *Argon2Authenticator) CredentialsAreValid(ctx context.Context, hash, password, totpSecret, totpCode string) (bool, error) {
	_, span := a.tracer.StartSpan(ctx)
	defer span.End()

	logger := a.logger.Clone()

	passwordMatches, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false, observability.PrepareError(err, span, "comparing argon2 hashed password")
	} else if !passwordMatches {
		return false, authentication.ErrPasswordDoesNotMatch
	}

	if totpSecret != "" && totpCode != "" {
		if !totp.Validate(totpCode, totpSecret) {
			logger.WithValues(map[string]any{
				"password_matches": passwordMatches,
				"provided_code":    totpCode,
			}).Debug("invalid code provided")

			return passwordMatches, authentication.ErrInvalidTOTPToken
		}
	}

	return passwordMatches, nil
}
