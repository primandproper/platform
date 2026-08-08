package tokenscfg

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/primandproper/platform-go/v10/authentication/tokens"
	"github.com/primandproper/platform-go/v10/authentication/tokens/jwt"
	"github.com/primandproper/platform-go/v10/authentication/tokens/paseto"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderJWT represents JWT.
	ProviderJWT = "jwt"
	// ProviderPASETO represents PASETO.
	ProviderPASETO = "paseto"
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
		return nil, fmt.Errorf("decoding json web token signing key: %w", err)
	}

	if len(decryptedSigningKey) != 32 {
		return nil, fmt.Errorf("decoding json web token signing key must be 32 bytes")
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case ProviderJWT:
		return jwt.NewSigner(cfg.Issuer, cfg.Audience, decryptedSigningKey, jwt.WithLogger(logger), jwt.WithTracerProvider(tracerProvider))
	case ProviderPASETO:
		return paseto.NewSigner(cfg.Issuer, cfg.Audience, decryptedSigningKey, paseto.WithLogger(logger), paseto.WithTracerProvider(tracerProvider))
	default:
		return nil, fmt.Errorf("unknown token issuer provider: %q", cfg.Provider)
	}
}
