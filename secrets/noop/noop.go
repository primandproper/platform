// Package noop is the secrets.SecretSource that holds no secrets, and how it
// says so is the thing to know: GetSecret returns an empty string with a nil
// error, never secrets.ErrSecretNotFound.
//
// That sentinel exists precisely so a missing secret can be told apart from one
// whose value is legitimately empty, and this source collapses the distinction.
// For every name it is ever asked, it answers that the secret exists and is
// empty. A caller that branches on ErrSecretNotFound to fall back to a default
// will not take that branch; it will configure itself with empty credentials
// and discover the problem later and further away, as an authentication failure
// against whatever those credentials were for.
//
// secrets/config builds it for the "noop" provider name, which has to be given.
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
