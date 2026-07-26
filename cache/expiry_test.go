package cache

import (
	"testing"
	"time"

	"github.com/shoenig/test"
)

func TestEffectiveExpiry(T *testing.T) {
	T.Parallel()

	T.Run("no options takes the default", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, time.Hour, EffectiveExpiry(time.Hour))
	})

	T.Run("zero expiry takes the default", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, time.Hour, EffectiveExpiry(time.Hour, WithExpiry(0)))
	})

	T.Run("positive expiry overrides the default", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, time.Minute, EffectiveExpiry(time.Hour, WithExpiry(time.Minute)))
	})

	T.Run("NoExpiry resolves to never expire", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, time.Duration(0), EffectiveExpiry(time.Hour, WithExpiry(NoExpiry)))
	})

	T.Run("non-positive default means never expire", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, time.Duration(0), EffectiveExpiry(0))
		test.EqOp(t, time.Duration(0), EffectiveExpiry(NoExpiry))
	})

	T.Run("last option wins and nil options are ignored", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, time.Minute, EffectiveExpiry(time.Hour, nil, WithExpiry(time.Second), WithExpiry(time.Minute)))
	})
}
