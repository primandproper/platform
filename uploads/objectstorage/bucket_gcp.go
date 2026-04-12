package objectstorage

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// GCPCloudStorageProvider indicates we'd like to use the GCP adapter for blob objectstorage.
	GCPCloudStorageProvider = "gcp"
)

type (
	// GCPConfig configures an GCP-based objectstorage provider.
	GCPConfig struct {
		_ struct{} `json:"-"`

		BucketName string `env:"BUCKET_NAME" json:"bucketName"`
	}
)

var _ validation.ValidatableWithContext = (*GCPConfig)(nil)

// ValidateWithContext validates the GCPCloudStorageConfig.
func (c *GCPConfig) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, c,
		validation.Field(&c.BucketName, validation.Required),
	)
}
