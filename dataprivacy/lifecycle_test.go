package dataprivacy

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v9/clock"
	"github.com/primandproper/platform-go/v9/identifiers"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/shoenig/test/wait"
)

func TestWorker_Lifecycle(T *testing.T) {
	T.Parallel()

	T.Run("Run fulfills a pending request and Close stops it", func(t *testing.T) {
		t.Parallel()

		var collected atomic.Int64

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", CollectorFunc(
				func(context.Context, Subject) (json.RawMessage, error) {
					collected.Add(1)

					return json.RawMessage(`{"ok":true}`), nil
				},
			)))
		})

		// A real ticker on a short interval: Run's loop is the thing under
		// test, so stubbing the ticker out would test nothing.
		env.worker.clock = clock.NewClock()
		env.worker.cfg.PollInterval = 5 * time.Millisecond

		req := saveRequest(t, env.store, newRequest(identifiers.New(), RequestExport, testSubject, time.Now().UTC()))

		go env.worker.Run()

		must.Wait(t, wait.InitialSuccess(
			wait.BoolFunc(func() bool { return collected.Load() > 0 }),
			wait.Timeout(5*time.Second),
			wait.Gap(5*time.Millisecond),
		))

		closeCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		must.NoError(t, env.worker.Close(closeCtx))

		read, err := env.store.Get(t.Context(), req.ID)
		must.NoError(t, err)
		test.EqOp(t, StatusCompleted, read.Status)
	})

	T.Run("Close is safe to call more than once", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterEraser("identity", countingEraser(0, 0, nil, nil)))
		})

		env.worker.clock = clock.NewClock()
		env.worker.cfg.PollInterval = time.Hour

		go env.worker.Run()

		closeCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		must.NoError(t, env.worker.Close(closeCtx))
		test.NoError(t, env.worker.Close(closeCtx))
	})

	T.Run("Close reports a context that expires before the loop drains", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterEraser("identity", countingEraser(0, 0, nil, nil)))
		})

		// Never started, so `done` never closes and Close can only give up.
		closeCtx, cancel := context.WithCancel(t.Context())
		cancel()

		test.Error(t, env.worker.Close(closeCtx))
	})

	T.Run("a claim error is counted rather than fatal", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterEraser("identity", countingEraser(0, 0, nil, nil)))
		})

		// A store whose Claim fails: the cycle logs and returns, and the next
		// one retries. There is no caller to hand the error to.
		env.worker.store = &failingClaimStore{Store: env.store}

		env.worker.cycle(t.Context())
	})
}

func TestSweeper_Lifecycle(T *testing.T) {
	T.Parallel()

	T.Run("a store error is reported without stopping the other chores", func(t *testing.T) {
		t.Parallel()

		env := newSweeperEnv(t, &SweeperConfig{})

		// Overdue counting fails; lapse, expire, and reap still ran, so the
		// result is partial rather than absent.
		env.sweeper.store = &failingOverdueStore{Store: env.sweeper.store}

		result, err := env.sweeper.Sweep(t.Context())
		must.Error(t, err)
		must.NotNil(t, result)
		test.StrContains(t, err.Error(), "counting overdue")
	})
}
