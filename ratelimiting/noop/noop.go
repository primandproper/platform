package noop

import (
	"context"

	"github.com/primandproper/platform-go/v10/ratelimiting"
)

var _ ratelimiting.RateLimiter = (*RateLimiter)(nil)

// RateLimiter always allows requests.
type RateLimiter struct{}

// Allow always returns true.
func (n *RateLimiter) Allow(ctx context.Context, key string) (bool, error) {
	return true, nil
}

// Close is a no-op.
func (n *RateLimiter) Close() error {
	return nil
}

// NewRateLimiter returns a RateLimiter that never limits.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{}
}
