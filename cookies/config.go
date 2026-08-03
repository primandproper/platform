package cookies

import (
	"context"
	"fmt"
	"strings"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// SameSite policy values accepted by Config.SameSite (case-insensitive). An
// empty value defaults to Lax.
const (
	SameSiteLax    = "lax"
	SameSiteStrict = "strict"
	SameSiteNone   = "none"
)

type Config struct {
	Domain                string        `env:"DOMAIN"      json:"domain,omitempty"                yaml:"domain,omitempty"`
	Base64EncodedHashKey  string        `env:"HASH_KEY"    json:"base64EncodedHashKey,omitempty"  yaml:"base64EncodedHashKey,omitempty"`
	Base64EncodedBlockKey string        `env:"BLOCK_KEY"   json:"base64EncodedBlockKey,omitempty" yaml:"base64EncodedBlockKey,omitempty"`
	SameSite              string        `env:"SAME_SITE"   json:"sameSite,omitempty"              yaml:"sameSite,omitempty"`
	Lifetime              time.Duration `env:"LIFETIME"    json:"lifetime,omitempty"              yaml:"lifetime,omitempty"`
	SecureOnly            bool          `env:"SECURE_ONLY" json:"secureOnly,omitempty"            yaml:"secureOnly,omitempty"`
}

const minCookieLifetime = 5 * time.Minute

func (c *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, c,
		validation.Field(&c.Base64EncodedHashKey, validation.Required),
		validation.Field(&c.Base64EncodedBlockKey, validation.Required),
		validation.Field(&c.Lifetime, validation.Min(minCookieLifetime)),
		validation.Field(&c.SameSite, validation.By(c.validateSameSite)),
	)
}

// validateSameSite accepts an empty value (defaults to Lax) or any of the
// SameSite constants case-insensitively, and rejects SameSite=None unless
// SecureOnly is set.
func (c *Config) validateSameSite(any) error {
	switch strings.ToLower(c.SameSite) {
	case "", SameSiteLax, SameSiteStrict:
		return nil
	case SameSiteNone:
		if !c.SecureOnly {
			// Browsers silently drop a SameSite=None cookie that is not Secure.
			return fmt.Errorf("cookie SameSite=%s requires SecureOnly", SameSiteNone)
		}
		return nil
	default:
		return fmt.Errorf("unsupported cookie SameSite value %q", c.SameSite)
	}
}
