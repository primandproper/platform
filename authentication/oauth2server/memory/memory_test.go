package memory

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v10/authentication/oauth2server"
	"github.com/primandproper/platform-go/v10/authentication/oauth2server/oauth2servertest"
	"github.com/primandproper/platform-go/v10/clock"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/shoenig/test/wait"
)

// The conformance suite carries every behavior this store shares with the
// database one. What is left here is what is genuinely this store's own: the
// clock it stamps against, and the sweeper's own goroutine.
func TestStore_Conformance(T *testing.T) {
	T.Parallel()

	oauth2servertest.Run(T, func(tb testing.TB) oauth2server.Store {
		tb.Helper()

		s := NewStore()
		tb.Cleanup(func() { must.NoError(tb, s.Close()) })

		return s
	}, oauth2servertest.WithInstanceLocalState())
}

// fakeClock is a Clock whose time only moves when a test moves it.
type fakeClock struct {
	ticks chan time.Time
	now   time.Time
	mu    sync.Mutex
}

var _ clock.Clock = (*fakeClock)(nil)

func newFakeClock() *fakeClock {
	return &fakeClock{
		now:   time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC),
		ticks: make(chan time.Time, 1),
	}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Since(t time.Time) time.Duration                  { return c.Now().Sub(t) }
func (c *fakeClock) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }
func (c *fakeClock) NewTicker(_ time.Duration) clock.Ticker           { return fakeTicker{c: c} }

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// tick releases one sweep.
func (c *fakeClock) tick() { c.ticks <- c.Now() }

type fakeTicker struct{ c *fakeClock }

func (t fakeTicker) Chan() <-chan time.Time { return t.c.ticks }
func (t fakeTicker) Stop()                  {}

func TestStore_Clock(T *testing.T) {
	T.Parallel()

	T.Run("expiry is evaluated against the injected clock", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		c := newFakeClock()
		store := NewStore(WithClock(c))

		code := &oauth2server.AuthorizationCode{
			IssuedAt:  c.Now(),
			ExpiresAt: c.Now().Add(time.Minute),
			Hash:      oauth2server.Hash("code"),
			ClientID:  "client",
			Subject:   oauth2server.Subject{ID: "user"},
		}

		must.NoError(t, store.CreateAuthorizationCode(ctx, code))

		c.advance(2 * time.Minute)

		// No sleeping. The deadline is data and the clock is injected, so the
		// case that a real server reaches after sixty seconds is reached here
		// in none.
		_, err := store.ConsumeAuthorizationCode(ctx, code.Hash)
		test.ErrorIs(t, err, oauth2server.ErrExpired)
	})

	T.Run("a nil clock leaves the default in place", func(t *testing.T) {
		t.Parallel()

		store := NewStore(WithClock(nil))
		test.NotNil(t, store.clock)
	})
}

func TestStore_Sweeper(T *testing.T) {
	T.Parallel()

	T.Run("the sweeper removes dead records on a tick", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		c := newFakeClock()
		store := NewStore(WithClock(c), WithSweeper(ctx, time.Minute))

		code := &oauth2server.AuthorizationCode{
			IssuedAt:  c.Now(),
			ExpiresAt: c.Now().Add(time.Minute),
			Hash:      oauth2server.Hash("swept"),
			ClientID:  "client",
			Subject:   oauth2server.Subject{ID: "user"},
		}

		must.NoError(t, store.CreateAuthorizationCode(ctx, code))

		c.advance(2 * time.Minute)
		c.tick()

		// The row is gone rather than merely refused, which is the difference
		// the sweeper exists to make: without it the table grows by one row per
		// login attempt, forever.
		must.Wait(t, wait.InitialSuccess(
			wait.BoolFunc(func() bool {
				store.mu.Lock()
				defer store.mu.Unlock()

				return len(store.codes) == 0
			}),
			wait.Timeout(5*time.Second),
			wait.Gap(time.Millisecond),
		))
	})

	T.Run("no sweeper is started without a context or an interval", func(t *testing.T) {
		t.Parallel()

		// Neither of these may start a goroutine on a ticker that panics, which
		// is what a nil-context sweeper would do on its first tick.
		test.NotNil(t, NewStore(WithSweeper(nil, time.Minute))) //nolint:staticcheck // deliberate nil context
		test.NotNil(t, NewStore(WithSweeper(context.Background(), 0)))
	})
}
