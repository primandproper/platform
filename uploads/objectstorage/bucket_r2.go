package objectstorage

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// R2Provider indicates we'd like to use the Cloudflare R2 adapter for blob.
	R2Provider = "r2"
)

type (
	// R2Config configures a Cloudflare R2-based objectstorage provider.
	R2Config struct {
		_ struct{} `json:"-"`

		AccountID       string `env:"ACCOUNT_ID"        json:"accountID"`
		BucketName      string `env:"BUCKET_NAME"       json:"bucketName"`
		AccessKeyID     string `env:"ACCESS_KEY_ID"     json:"accessKeyID"`
		SecretAccessKey string `env:"SECRET_ACCESS_KEY" json:"secretAccessKey"`
	}
)

var _ validation.ValidatableWithContext = (*R2Config)(nil)

// ValidateWithContext validates the R2Config.
func (c *R2Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, c,
		validation.Field(&c.AccountID, validation.Required),
		validation.Field(&c.BucketName, validation.Required),
		validation.Field(&c.AccessKeyID, validation.Required),
		validation.Field(&c.SecretAccessKey, validation.Required),
	)
}
