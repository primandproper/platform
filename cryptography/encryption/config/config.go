package encryptioncfg

import (
	"context"
	"slices"
	"strings"

	"github.com/primandproper/platform-go/v9/cryptography/encryption"
	"github.com/primandproper/platform-go/v9/cryptography/encryption/aes"
	"github.com/primandproper/platform-go/v9/cryptography/encryption/salsa20"
	perrors "github.com/primandproper/platform-go/v9/errors"

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

// providers are every provider this package implements. Validation and the
// dispatch switch both read it, so they cannot drift apart.
var providers = []string{ProviderAES, ProviderSalsa20}

// normalize canonicalizes a provider name the way the dispatch switch does.
func normalize(provider string) string {
	return strings.TrimSpace(strings.ToLower(provider))
}

// ValidateWithContext validates a Config struct.
//
// It checks the normalized provider, not the raw string: dispatch lowercases
// and trims, so validating the raw value rejected "AES" and " aes " while the
// factory accepted both.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.Required, validation.By(func(any) error {
			if !slices.Contains(providers, normalize(cfg.Provider)) {
				return perrors.Wrapf(perrors.ErrUnknownProvider, "encryption provider %q", cfg.Provider)
			}

			return nil
		})),
	)
}

// NewEncryptorDecryptor provides an EncryptorDecryptor based on the configured provider.
func NewEncryptorDecryptor(
	ctx context.Context,
	cfg *Config,
	key []byte,
	opts ...Option,
) (encryption.EncryptorDecryptor, error) {
	o := newOptions(opts)
	logger, tracerProvider := o.logger, o.tracerProvider

	if cfg == nil {
		return nil, perrors.ErrNilInputProvided
	}

	switch normalize(cfg.Provider) {
	case ProviderAES:
		return aes.NewEncryptorDecryptor(key, aes.WithLogger(logger), aes.WithTracerProvider(tracerProvider))
	case ProviderSalsa20:
		return salsa20.NewEncryptorDecryptor(key, salsa20.WithLogger(logger), salsa20.WithTracerProvider(tracerProvider))
	default:
		return nil, perrors.Wrapf(perrors.ErrUnknownProvider, "encryption provider %q", cfg.Provider)
	}
}
