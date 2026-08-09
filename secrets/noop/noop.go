package noop

import (
	"context"

	"github.com/primandproper/platform-go/v10/secrets"
)

var _ secrets.SecretSource = (*SecretSource)(nil)

// SecretSource returns empty string for all secrets.
type SecretSource struct{}

// GetSecret returns empty string.
func (n *SecretSource) GetSecret(ctx context.Context, name string) (string, error) {
	return "", nil
}

// Close is a no-op.
func (n *SecretSource) Close() error {
	return nil
}

// NewSecretSource returns a SecretSource that returns empty strings.
func NewSecretSource() *SecretSource {
	return &SecretSource{}
}
