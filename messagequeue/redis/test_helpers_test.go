package redis

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform/testutils/containers"

	"github.com/testcontainers/testcontainers-go"
	rediscontainers "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	redisProtocolPrefix      = "redis://"
	redisContainerImageToUse = "redis:7-bullseye"
)

func BuildContainerBackedRedisConfigForTest(t *testing.T) (config *Config, shutdownFunc func(context.Context) error, err error) {
	t.Helper()

	cfg, sdf, err := BuildContainerBackedRedisConfig(t.Context())
	if err != nil {
		return nil, nil, err
	}

	return cfg, sdf, nil
}

func BuildContainerBackedRedisConfig(ctx context.Context) (config *Config, shutdownFunc func(context.Context) error, err error) {
	redisContainer, err := containers.StartWithRetry(ctx, func(ctx context.Context) (*rediscontainers.RedisContainer, error) {
		return rediscontainers.Run(
			ctx,
			redisContainerImageToUse,
			rediscontainers.WithLogLevel(rediscontainers.LogLevelNotice),
			testcontainers.WithWaitStrategyAndDeadline(2*time.Minute, wait.ForAll(
				wait.ForListeningPort("6379/tcp"),
				wait.ForLog("Ready to accept connections"),
			)),
		)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build redis container: %w", err)
	}

	redisAddress, err := redisContainer.ConnectionString(ctx)
	if err != nil {
		if termErr := redisContainer.Terminate(ctx); termErr != nil {
			slog.Error("failed to terminate redis container", slog.Any("error", termErr))
		}
		return nil, nil, fmt.Errorf("failed to build redis connection string: %w", err)
	}

	cfg := &Config{
		QueueAddresses: []string{strings.TrimPrefix(redisAddress, redisProtocolPrefix)},
	}

	shutdownFunction := func(shutdownCtx context.Context) error {
		return redisContainer.Terminate(shutdownCtx)
	}

	return cfg, shutdownFunction, nil
}
