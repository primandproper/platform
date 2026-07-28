// Package containers provides shared helpers for starting testcontainers
// with uniform retry behavior. It exists so every container builder in the
// repo can opt into the same backoff policy instead of each rolling its own.
//
// Container startup flakes for many non-deterministic reasons — Docker daemon
// cold starts, port conflicts, image pull stalls, transient network blips —
// and a single attempt is too brittle for a large integration test suite.
package containers

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v8/retry"

	"github.com/shoenig/test/must"
	"github.com/testcontainers/testcontainers-go"
)

const (
	defaultMaxAttempts  = 5
	defaultInitialDelay = time.Second

	// DefaultShutdownTimeout bounds how long Run waits for a container to
	// terminate. Termination happens on a fresh context, so a test that has
	// already blown its own deadline still gets its container reaped.
	DefaultShutdownTimeout = 30 * time.Second
)

// RunningTests reports whether RUN_CONTAINER_TESTS=true is set in the
// environment. Container-backed tests across the repo should gate on this
// (typically via `if !containers.RunningTests { t.SkipNow() }`) so a default
// `go test ./...` does not require a Docker daemon. The variable is read once
// at package init.
var RunningTests = strings.TrimSpace(strings.ToLower(os.Getenv("RUN_CONTAINER_TESTS"))) == "true"

// SkipIfNotRunning skips the current test or benchmark (via SkipNow) when
// RunningTests is false. It is the one-line equivalent of `if
// !containers.RunningTests { tb.SkipNow() }` that every container-backed test
// and benchmark in the repo needs. It accepts testing.TB so both *testing.T
// and *testing.B can use it.
func SkipIfNotRunning(tb testing.TB) {
	tb.Helper()
	if !RunningTests {
		tb.SkipNow()
	}
}

// DefaultRetryConfig returns the retry.Config used by StartWithRetry. Callers
// that need bespoke retry behavior can start from this and tweak individual
// fields before calling retry.NewExponentialBackoffPolicy themselves.
func DefaultRetryConfig() retry.Config {
	return retry.Config{
		MaxAttempts:  defaultMaxAttempts,
		InitialDelay: defaultInitialDelay,
		UseJitter:    false,
	}
}

// StartWithRetry invokes start with exponential backoff retry on failure. It
// is a thin wrapper over the retry package so that every container builder in
// the repo gets the same backoff policy for free.
//
// The callback receives the same ctx that was passed in, and is expected to
// return the concrete container type from its module's Run function (e.g.
// *postgres.PostgresContainer, *redis.RedisContainer). Callers handle the
// error themselves — typically via must.NoError(t, err) — so that this helper
// stays decoupled from the testing package.
func StartWithRetry[C any](ctx context.Context, start func(context.Context) (C, error)) (C, error) {
	var container C
	policy := retry.NewExponentialBackoffPolicy(DefaultRetryConfig())
	err := policy.Execute(ctx, func(ctx context.Context) error {
		var startErr error
		container, startErr = start(ctx)
		return startErr
	})
	return container, err
}

// Terminable is the teardown half of the testcontainers container API — the only
// thing Run needs in order to own a container's lifecycle. Every module container
// type (*postgres.PostgresContainer, *redis.RedisContainer, …) satisfies it, as
// does testcontainers.Container itself.
type Terminable interface {
	Terminate(ctx context.Context, opts ...testcontainers.TerminateOption) error
}

// Run starts a container and hands it to fn, owning everything around the closure
// so the test body only has to say what it wants done with a live container. It is
// to container-backed tests what database.RunInTransaction is to transactions: the
// caller supplies the work, the helper supplies the lifecycle.
//
// Everything a container-backed test in this repo has to remember is handled here:
//
//   - the RUN_CONTAINER_TESTS gate, so a bare `go test ./...` skips instead of
//     demanding a Docker daemon.
//   - startup via StartWithRetry, so the shared backoff policy applies, and a
//     startup failure fails the test rather than yielding a nil container.
//   - termination, once, whatever fn does — return, t.Fatal, or panic.
//
// fn receives the container itself along with tb.Context(); it is not handed a
// shutdown closure, because it does not own shutdown.
//
// Termination is registered with tb.Cleanup rather than deferred until fn returns.
// That distinction is load-bearing: a closure that registers parallel subtests
// returns *before* those subtests execute, and a deferred Terminate would pull the
// container out from under them.
//
// The flip side is that the container lives until the end of tb, not the end of fn,
// so call Run from the narrowest test that needs the container rather than hoisting
// it up to a parent that runs unrelated work afterwards.
func Run[C Terminable](tb testing.TB, start func(ctx context.Context) (C, error), fn func(ctx context.Context, container C)) {
	tb.Helper()

	if start == nil || fn == nil {
		tb.Fatal("containers: Run requires a non-nil start and fn")
	}

	SkipIfNotRunning(tb)

	ctx := tb.Context()

	container, err := StartWithRetry(ctx, start)
	must.NoError(tb, err)

	tb.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), DefaultShutdownTimeout)
		defer cancel()

		if terminateErr := container.Terminate(shutdownCtx); terminateErr != nil {
			tb.Logf("containers: terminating container: %v", terminateErr)
		}
	})

	fn(ctx, container)
}
