package database

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestWithSweeper(T *testing.T) {
	T.Parallel()

	// The wall clock is deliberate rather than the fake one the other tests
	// use: inside a synctest bubble clock.NewClock reads the bubble's time, so
	// the sweeper's ticker advances with time.Sleep and needs no test double.
	T.Run("removes expired rows with no Consume to discover them", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			client := newTestClient(t)

			store, err := NewSessionStore(&Config{}, client, WithSweeper(t.Context(), 10*time.Second))
			must.NoError(t, err)

			must.NoError(t, store.Save(t.Context(), testSession("abandoned"), time.Minute))
			test.EqOp(t, 1, rowCount(t, store))

			// Past the row's deadline and across at least one tick. This is the
			// ceremony nobody finished, which is the row nothing else deletes.
			time.Sleep(time.Minute + 10*time.Second)
			synctest.Wait()

			test.EqOp(t, 0, rowCount(t, store))
		})
	})

	T.Run("leaves live rows alone", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			client := newTestClient(t)

			store, err := NewSessionStore(&Config{}, client, WithSweeper(t.Context(), time.Second))
			must.NoError(t, err)

			must.NoError(t, store.Save(t.Context(), testSession("live"), time.Hour))

			time.Sleep(time.Minute)
			synctest.Wait()

			test.EqOp(t, 1, rowCount(t, store))
		})
	})

	// The goroutine's life is the caller's to bound. A sweeper that outlived
	// the scope that started it would keep a client alive after Close.
	T.Run("stops when its context is done", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())

			client := newTestClient(t)

			store, err := NewSessionStore(&Config{}, client, WithSweeper(ctx, time.Second))
			must.NoError(t, err)

			cancel()
			synctest.Wait()

			must.NoError(t, store.Save(t.Context(), testSession("orphan"), time.Second))

			time.Sleep(time.Minute)
			synctest.Wait()

			// Still there, because nothing is sweeping any more.
			test.EqOp(t, 1, rowCount(t, store))
		})
	})

	T.Run("starts nothing without a context or an interval", func(t *testing.T) {
		t.Parallel()

		o := newOptions([]Option{WithSweeper(nil, time.Second)}) //nolint:staticcheck // deliberate nil context
		must.Nil(t, o.sweepCtx)

		o = newOptions([]Option{WithSweeper(context.Background(), 0)})
		must.Nil(t, o.sweepCtx)
	})
}
