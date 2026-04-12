package secrets

import "context"

// SecretSource provides access to secrets.
type SecretSource interface {
	GetSecret(ctx context.Context, name string) (string, error)
	Close() error
}
