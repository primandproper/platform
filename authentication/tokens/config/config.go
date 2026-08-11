package tokenscfg

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/primandproper/platform-go/v10/authentication/tokens"
	"github.com/primandproper/platform-go/v10/authentication/tokens/jwt"
	"github.com/primandproper/platform-go/v10/authentication/tokens/paseto"
	"github.com/primandproper/platform-go/v10/errors"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderJWT represents JWT.
	ProviderJWT = "jwt"
	// ProviderPASETO represents PASETO.
	ProviderPASETO = "paseto"

	// signingKeyLength is how many bytes both signers require of a decoded
	// signing key.
	signingKeyLength = 32
)

type (
	// Config is the configuration structure.
	Config struct {
		Provider                string `env:"PROVIDER"    json:"provider,omitempty"                yaml:"provider,omitempty"`
		Issuer                  string `env:"ISSUER"      json:"issuer,omitempty"                  yaml:"issuer,omitempty"`
		Audience                string `env:"AUDIENCE"    json:"audience,omitempty"                yaml:"audience,omitempty"`
		Base64EncodedSigningKey string `env:"SIGNING_KEY" json:"base64EncodedSigningKey,omitempty" yaml:"base64EncodedSigningKey,omitempty"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(
		ctx,
		cfg,
		validation.Field(&cfg.Provider, validation.Required, validation.In(ProviderJWT, ProviderPASETO)),
		validation.Field(&cfg.Issuer, validation.Required),
		validation.Field(&cfg.Audience, validation.Required),
		validation.Field(&cfg.Base64EncodedSigningKey, validation.Required),
	)
}

// NewTokenIssuer provides a token issuer.
func (cfg *Config) NewTokenIssuer(opts ...Option) (tokens.Issuer, error) {
	o := newOptions(opts)
	logger, tracerProvider := o.logger, o.tracerProvider

	decryptedSigningKey, err := base64.URLEncoding.DecodeString(cfg.Base64EncodedSigningKey)
	if err != nil {
		return nil, errors.Wrap(err, "decoding token signing key")
	}

	if len(decryptedSigningKey) != signingKeyLength {
		return nil, errors.Newf("token signing key must be %d bytes, got %d", signingKeyLength, len(decryptedSigningKey))
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case ProviderJWT:
		return jwt.NewSigner(cfg.Issuer, cfg.Audience, decryptedSigningKey, jwt.WithLogger(logger), jwt.WithTracerProvider(tracerProvider))
	case ProviderPASETO:
		return paseto.NewSigner(cfg.Issuer, cfg.Audience, decryptedSigningKey, paseto.WithLogger(logger), paseto.WithTracerProvider(tracerProvider))
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "token issuer provider %q", cfg.Provider)
	}
}
