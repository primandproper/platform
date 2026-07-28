package jobs_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/primandproper/platform-go/v8/distributedlock"
	"github.com/primandproper/platform-go/v8/distributedlock/memory"
	dlmock "github.com/primandproper/platform-go/v8/distributedlock/mock"
	"github.com/primandproper/platform-go/v8/jobs"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// testInterval is long enough that no assertion below could be satisfied by an
// accidental second tick, and free either way: every scheduler test runs inside
// a synctest bubble, where the wait costs no wall time.
const testInterval = time.Minute

// errJob is the failure the scheduler tests return from a job.
var errJob = errors.New("job exploded")

func newTestLocker(t *testing.T) distributedlock.Locker {
	t.Helper()

	locker, err := memory.NewLocker(nil, nil, nil)
	must.NoError(t, err)

	return locker
}

// newTestScheduler builds a Scheduler over a real in-memory lock. It is a real
// lock rather than a noop precisely because mutual exclusion is the behavior
// under test.
func newTestScheduler(t *testing.T, opts ...jobs.SchedulerOption) *jobs.Scheduler {
	t.Helper()

	scheduler, err := jobs.NewScheduler(&jobs.SchedulerConfig{}, newTestLocker(t), opts...)
	must.NoError(t, err)
	must.NotNil(t, scheduler)

	return scheduler
}

// runScheduler starts a Scheduler inside a bubble and closes it when the test
// ends, so a bubble never exits with a goroutine still running in it.
func runScheduler(t *testing.T, scheduler *jobs.Scheduler) {
	t.Helper()

	go scheduler.Run()

	t.Cleanup(func() {
		must.NoError(t, scheduler.Close(context.Background()))
	})
}

func TestNewScheduler(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.NotNil(t, newTestScheduler(t))
	})

	T.Run("with nil config", func(t *testing.T) {
		t.Parallel()

		_, err := jobs.NewScheduler(nil, newTestLocker(t))
		test.Error(t, err)
	})

	T.Run("with nil locker", func(t *testing.T) {
		t.Parallel()

		_, err := jobs.NewScheduler(&jobs.SchedulerConfig{}, nil)
		test.ErrorIs(t, err, jobs.ErrNilLocker)
	})

	T.Run("with a sub-second lease TTL", func(t *testing.T) {
		t.Parallel()

		_, err := jobs.NewScheduler(&jobs.SchedulerConfig{DefaultLeaseTTL: time.Millisecond}, newTestLocker(t))
		test.Error(t, err)
	})
}

func TestSchedulerConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("fills unset knobs", func(t *testing.T) {
		t.Parallel()

		cfg := &jobs.SchedulerConfig{}
		cfg.EnsureDefaults()

		test.EqOp(t, jobs.DefaultLockKeyPrefix, cfg.LockKeyPrefix)
		test.EqOp(t, jobs.DefaultLeaseTTL, cfg.DefaultLeaseTTL)
		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestScheduler_Register(T *testing.T) {
	T.Parallel()

	noop := func(context.Context) error { return nil }

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, newTestScheduler(t).Register(
			jobs.Job{Name: "one", Interval: testInterval, Run: noop},
			jobs.Job{Name: "two", Interval: testInterval, Run: noop},
		))
	})

	T.Run("with an empty name", func(t *testing.T) {
		t.Parallel()

		err := newTestScheduler(t).Register(jobs.Job{Interval: testInterval, Run: noop})
		test.ErrorIs(t, err, jobs.ErrInvalidJob)
	})

	T.Run("with no function", func(t *testing.T) {
		t.Parallel()

		err := newTestScheduler(t).Register(jobs.Job{Name: "one", Interval: testInterval})
		test.ErrorIs(t, err, jobs.ErrInvalidJob)
	})

	T.Run("with a non-positive interval", func(t *testing.T) {
		t.Parallel()

		err := newTestScheduler(t).Register(jobs.Job{Name: "one", Run: noop})
		test.ErrorIs(t, err, jobs.ErrInvalidJob)
	})

	T.Run("with a duplicate name", func(t *testing.T) {
		t.Parallel()

		scheduler := newTestScheduler(t)
		must.NoError(t, scheduler.Register(jobs.Job{Name: "one", Interval: testInterval, Run: noop}))

		err := scheduler.Register(jobs.Job{Name: "one", Interval: testInterval, Run: noop})
		test.ErrorIs(t, err, jobs.ErrDuplicateJob)
	})

	T.Run("with a name duplicated within one batch", func(t *testing.T) {
		t.Parallel()

		err := newTestScheduler(t).Register(
			jobs.Job{Name: "one", Interval: testInterval, Run: noop},
			jobs.Job{Name: "one", Interval: testInterval, Run: noop},
		)
		test.ErrorIs(t, err, jobs.ErrDuplicateJob)
	})

	T.Run("rejects the whole batch when one job is invalid", func(t *testing.T) {
		t.Parallel()

		scheduler := newTestScheduler(t)

		test.Error(t, scheduler.Register(
			jobs.Job{Name: "good", Interval: testInterval, Run: noop},
			jobs.Job{Name: "bad", Run: noop},
		))

		// "good" was not kept, so registering it again is not a duplicate.
		test.NoError(t, scheduler.Register(jobs.Job{Name: "good", Interval: testInterval, Run: noop}))
	})

	T.Run("after Run", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			scheduler := newTestScheduler(t)
			runScheduler(t, scheduler)
			synctest.Wait()

			err := scheduler.Register(jobs.Job{Name: "late", Interval: testInterval, Run: noop})
			test.ErrorIs(t, err, jobs.ErrSchedulerRunning)
		})
	})
}

func TestScheduler_Run(T *testing.T) {
	T.Parallel()

	T.Run("fires on the interval", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ran := make(chan time.Time, 4)

			scheduler := newTestScheduler(t)
			must.NoError(t, scheduler.Register(jobs.Job{
				Name:     "ticker",
				Interval: testInterval,
				Run: func(context.Context) error {
					ran <- time.Now()

					return nil
				},
			}))

			start := time.Now()
			runScheduler(t, scheduler)

			first := <-ran
			second := <-ran

			test.EqOp(t, testInterval, first.Sub(start))
			test.EqOp(t, testInterval, second.Sub(first))
		})
	})

	T.Run("RunOnStart fires without waiting an interval", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ran := make(chan time.Time, 1)

			scheduler := newTestScheduler(t)
			must.NoError(t, scheduler.Register(jobs.Job{
				Name:       "eager",
				Interval:   testInterval,
				RunOnStart: true,
				Run: func(context.Context) error {
					ran <- time.Now()

					return nil
				},
			}))

			start := time.Now()
			runScheduler(t, scheduler)

			test.EqOp(t, start, <-ran)
		})
	})

	T.Run("runs registered jobs independently", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			fast := make(chan struct{}, 8)
			slow := make(chan struct{}, 8)

			scheduler := newTestScheduler(t)
			must.NoError(t, scheduler.Register(
				jobs.Job{Name: "fast", Interval: time.Second, Run: func(context.Context) error {
					fast <- struct{}{}

					return nil
				}},
				jobs.Job{Name: "slow", Interval: time.Hour, Run: func(context.Context) error {
					slow <- struct{}{}

					return nil
				}},
			))

			runScheduler(t, scheduler)

			<-fast
			<-fast
			test.SliceEmpty(t, drain(slow))
		})
	})

	T.Run("a failing job keeps its schedule", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			spy := newCounterSpy()
			ran := make(chan struct{}, 4)

			scheduler := newTestScheduler(t, jobs.WithSchedulerMetricsProvider(spy.provider()))
			must.NoError(t, scheduler.Register(jobs.Job{
				Name:     "flaky",
				Interval: testInterval,
				Run: func(context.Context) error {
					ran <- struct{}{}

					return errJob
				},
			}))

			runScheduler(t, scheduler)

			// A second tick is the claim: an error is not terminal, and the
			// next tick is the retry.
			<-ran
			<-ran
			synctest.Wait()

			test.EqOp(t, int64(2), spy.count("jobs_scheduler_failures"))
			test.EqOp(t, int64(2), spy.count("jobs_scheduler_runs"))
		})
	})

	T.Run("contains a panicking job and keeps its schedule", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			spy := newCounterSpy()
			var runs atomic.Int64
			ran := make(chan struct{}, 4)

			scheduler := newTestScheduler(t, jobs.WithSchedulerMetricsProvider(spy.provider()))
			must.NoError(t, scheduler.Register(jobs.Job{
				Name:     "exploder",
				Interval: testInterval,
				Run: func(context.Context) error {
					defer func() { ran <- struct{}{} }()

					if runs.Add(1) == 1 {
						panic("job blew up")
					}

					return nil
				},
			}))

			runScheduler(t, scheduler)

			<-ran
			// The second tick is the proof: an uncontained panic would have
			// unwound this job's goroutine and stopped it for good.
			<-ran

			test.EqOp(t, int64(1), spy.count("jobs_scheduler_panics"))
			test.EqOp(t, int64(1), spy.count("jobs_scheduler_failures"))
		})
	})

	T.Run("bounds a run with its timeout", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			observed := make(chan error, 1)

			scheduler := newTestScheduler(t)
			must.NoError(t, scheduler.Register(jobs.Job{
				Name:       "wedged",
				Interval:   time.Hour,
				Timeout:    testInterval,
				LeaseTTL:   time.Hour,
				RunOnStart: true,
				Run: func(ctx context.Context) error {
					<-ctx.Done()
					observed <- ctx.Err()

					return ctx.Err()
				},
			}))

			runScheduler(t, scheduler)

			test.ErrorIs(t, <-observed, context.DeadlineExceeded)
		})
	})
}

func TestScheduler_Leasing(T *testing.T) {
	T.Parallel()

	T.Run("only one replica runs a job per tick", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			spy := newCounterSpy()

			// One locker shared by two schedulers is what "two replicas" means
			// here: the lock, not the process boundary, is what coordinates.
			locker := newTestLocker(t)

			started := make(chan string, 4)
			release := make(chan struct{})

			for _, replica := range []string{"a", "b"} {
				scheduler, err := jobs.NewScheduler(&jobs.SchedulerConfig{}, locker,
					jobs.WithSchedulerMetricsProvider(spy.provider()))
				must.NoError(t, err)

				must.NoError(t, scheduler.Register(jobs.Job{
					Name:     "singleton",
					Interval: testInterval,
					Run: func(context.Context) error {
						started <- replica
						<-release

						return nil
					},
				}))

				runScheduler(t, scheduler)
			}

			// The winner is now inside the job, holding the lease.
			<-started

			// Wait for the loser to finish its tick, which it can only do by
			// failing to acquire and giving up.
			synctest.Wait()
			test.SliceEmpty(t, drain(started))
			test.EqOp(t, int64(1), spy.count("jobs_scheduler_skipped"))
			test.EqOp(t, int64(1), spy.count("jobs_scheduler_runs"))

			close(release)
		})
	})

	T.Run("counts a lease that expired mid-run", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			spy := newCounterSpy()
			ran := make(chan struct{}, 1)

			locker := &dlmock.LockerMock{
				AcquireFunc: func(context.Context, string, time.Duration) (distributedlock.Lock, error) {
					return &dlmock.LockMock{
						ReleaseFunc: func(context.Context) error {
							return distributedlock.ErrLockNotHeld
						},
					}, nil
				},
			}

			scheduler, err := jobs.NewScheduler(&jobs.SchedulerConfig{}, locker,
				jobs.WithSchedulerMetricsProvider(spy.provider()))
			must.NoError(t, err)

			must.NoError(t, scheduler.Register(jobs.Job{
				Name:       "overrunner",
				Interval:   time.Hour,
				RunOnStart: true,
				Run: func(context.Context) error {
					ran <- struct{}{}

					return nil
				},
			}))

			runScheduler(t, scheduler)

			<-ran
			synctest.Wait()

			test.EqOp(t, int64(1), spy.count("jobs_scheduler_leases_expired"))
		})
	})

	T.Run("a lock failure skips the tick without running the job", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			spy := newCounterSpy()
			var runs atomic.Int64

			locker := &dlmock.LockerMock{
				AcquireFunc: func(context.Context, string, time.Duration) (distributedlock.Lock, error) {
					return nil, errors.New("lock backend is down")
				},
			}

			scheduler, err := jobs.NewScheduler(&jobs.SchedulerConfig{}, locker,
				jobs.WithSchedulerMetricsProvider(spy.provider()))
			must.NoError(t, err)

			must.NoError(t, scheduler.Register(jobs.Job{
				Name:       "unreachable",
				Interval:   time.Hour,
				RunOnStart: true,
				Run: func(context.Context) error {
					runs.Add(1)

					return nil
				},
			}))

			runScheduler(t, scheduler)
			synctest.Wait()

			test.EqOp(t, int64(0), runs.Load())
			test.EqOp(t, int64(1), spy.count("jobs_scheduler_lock_errors"))
			test.EqOp(t, int64(0), spy.count("jobs_scheduler_skipped"))
		})
	})
}

func TestScheduler_Close(T *testing.T) {
	T.Parallel()

	T.Run("waits for the in-flight run", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			finished := make(chan struct{})

			scheduler := newTestScheduler(t)
			must.NoError(t, scheduler.Register(jobs.Job{
				Name:       "slow",
				Interval:   time.Hour,
				LeaseTTL:   time.Hour,
				RunOnStart: true,
				Run: func(context.Context) error {
					entered <- struct{}{}
					<-release
					close(finished)

					return nil
				},
			}))

			go scheduler.Run()
			<-entered

			closed := make(chan error, 1)
			go func() { closed <- scheduler.Close(context.Background()) }()

			synctest.Wait()
			test.SliceEmpty(t, drain(closed))

			close(release)

			must.NoError(t, <-closed)
			<-finished
		})
	})

	T.Run("reports the deadline when a run outlasts it", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			release := make(chan struct{})

			scheduler := newTestScheduler(t)
			must.NoError(t, scheduler.Register(jobs.Job{
				Name:       "wedged",
				Interval:   time.Hour,
				LeaseTTL:   time.Hour,
				RunOnStart: true,
				Run: func(context.Context) error {
					entered <- struct{}{}
					<-release

					return nil
				},
			}))

			go scheduler.Run()
			<-entered

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			test.ErrorIs(t, scheduler.Close(ctx), context.DeadlineExceeded)

			// Release afterwards so the bubble is not left with a parked
			// goroutine in it.
			close(release)
			must.NoError(t, scheduler.Close(context.Background()))
		})
	})

	T.Run("is safe to call more than once", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			scheduler := newTestScheduler(t)
			go scheduler.Run()
			synctest.Wait()

			test.NoError(t, scheduler.Close(context.Background()))
			test.NoError(t, scheduler.Close(context.Background()))
		})
	})
}

// drain empties ch without blocking, so a test can assert that nothing arrived
// on it. A blocking receive would let a synctest bubble idle forward to the
// next tick and pass regardless.
func drain[T any](ch <-chan T) []T {
	var received []T

	for {
		select {
		case v := <-ch:
			received = append(received, v)
		default:
			return received
		}
	}
}
