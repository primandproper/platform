package asynccfg

import (
	"context"

	"github.com/primandproper/platform-go/v11/errors"
	"github.com/primandproper/platform-go/v11/notifications/async"
)

// NewAsyncNotifier provides an AsyncNotifier from a config.
func NewAsyncNotifier(ctx context.Context, cfg *Config, opts ...Option) (async.AsyncNotifier, error) {
	if cfg == nil {
		return nil, errors.ErrNilInputParameter
	}

	return cfg.NewAsyncNotifier(ctx, opts...)
}
