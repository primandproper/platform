package cookies

import (
	"context"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type Config struct {
	Domain                string        `env:"DOMAIN"      json:"domain"`
	CookieName            string        `env:"COOKIE_NAME" json:"cookieName"`
	Base64EncodedHashKey  string        `env:"HASH_KEY"    json:"base64EncodedHashKey"`
	Base64EncodedBlockKey string        `env:"BLOCK_KEY"   json:"base64EncodedBlockKey"`
	Lifetime              time.Duration `env:"LIFETIME"    json:"lifetime"`
	SecureOnly            bool          `env:"SECURE_ONLY" json:"secureOnly"`
}

const minCookieLifetime = 5 * time.Minute

func (c *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, c,
		validation.Field(&c.CookieName, validation.Required),
		validation.Field(&c.Base64EncodedHashKey, validation.Required),
		validation.Field(&c.Base64EncodedBlockKey, validation.Required),
		validation.Field(&c.Lifetime, validation.Min(minCookieLifetime)),
	)
}
