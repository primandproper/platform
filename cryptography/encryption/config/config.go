package config

import (
	"context"
	"strings"

	"github.com/primandproper/platform-go/v9/cryptography/encryption"
	"github.com/primandproper/platform-go/v9/cryptography/encryption/aes"
	"github.com/primandproper/platform-go/v9/cryptography/encryption/salsa20"
	perrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderAES is the AES-GCM encryption provider.
	ProviderAES = "aes"
	// ProviderSalsa20 is the Salsa20 encryption provider.
	ProviderSalsa20 = "salsa20"
)

type (
	// Config is the configuration for the encryption provider.
	Config struct {
		Provider string `env:"PROVIDER" json:"provider" yaml:"provider"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.Required, validation.In(ProviderAES, ProviderSalsa20)),
	)
}

// NewEncryptorDecryptor provides an EncryptorDecryptor based on the configured provider.
func NewEncryptorDecryptor(
	cfg *Config,
	tracerProvider tracing.TracerProvider,
	logger logging.Logger,
	key []byte,
) (encryption.EncryptorDecryptor, error) {
	if cfg == nil {
		return nil, perrors.ErrNilInputProvided
	}

	switch strings.TrimSpace(strings.ToLower(cfg.Provider)) {
	case ProviderAES:
		return aes.NewEncryptorDecryptor(key, aes.WithLogger(logger), aes.WithTracerProvider(tracerProvider))
	case ProviderSalsa20:
		return salsa20.NewEncryptorDecryptor(key, salsa20.WithLogger(logger), salsa20.WithTracerProvider(tracerProvider))
	default:
		return nil, perrors.Newf("unknown encryption provider: %q", cfg.Provider)
	}
}
