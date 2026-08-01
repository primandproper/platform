package llm

import (
	"errors"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRateLimitError(T *testing.T) {
	T.Parallel()

	T.Run("matches the sentinel", func(t *testing.T) {
		t.Parallel()

		err := error(&RateLimitError{RetryAfter: 3 * time.Second})

		must.ErrorIs(t, err, ErrRateLimited)
	})

	T.Run("survives wrapping", func(t *testing.T) {
		t.Parallel()

		err := errors.Join(errors.New("context"), &RateLimitError{})

		must.ErrorIs(t, err, ErrRateLimited)

		rateLimit, ok := errors.AsType[*RateLimitError](err)
		must.True(t, ok)
		test.EqOp(t, time.Duration(0), rateLimit.RetryAfter)
	})

	T.Run("mentions the retry advice when there is some", func(t *testing.T) {
		t.Parallel()

		err := &RateLimitError{RetryAfter: 90 * time.Second}

		test.StrContains(t, err.Error(), ErrRateLimited.Error())
		test.StrContains(t, err.Error(), "1m30s")
	})

	T.Run("reads as the plain sentinel when there is none", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, ErrRateLimited.Error(), (&RateLimitError{}).Error())
	})
}

func TestSentinels(T *testing.T) {
	T.Parallel()

	T.Run("are distinct from one another", func(t *testing.T) {
		t.Parallel()

		// A sentinel accidentally aliased to another would let a caller branch
		// on the wrong condition, and every one of these is load-bearing at a
		// call site that has to tell "retry me" from "never retry me".
		sentinels := []error{
			ErrRateLimited,
			ErrContextTooLong,
			ErrAuthentication,
			ErrModelNotFound,
			ErrContentFiltered,
			ErrInvalidRequest,
			ErrUnsupportedFeature,
		}

		for i, a := range sentinels {
			for j, b := range sentinels {
				if i == j {
					continue
				}

				test.False(t, errors.Is(a, b), test.Sprintf("sentinel %d matches sentinel %d", i, j))
			}
		}
	})
}
