package asynccfg

import (
	"context"

	"github.com/primandproper/platform-go/v9/notifications/async"
)

// NewAsyncNotifier provides an AsyncNotifier from a config.
func NewAsyncNotifier(_ context.Context, cfg *Config, opts ...Option) (async.AsyncNotifier, error) {
	return cfg.NewAsyncNotifier(opts...)
}
