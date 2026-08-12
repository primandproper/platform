package messagequeue

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewPublishOptions(T *testing.T) {
	T.Parallel()

	T.Run("with no options", func(t *testing.T) {
		t.Parallel()

		actual := NewPublishOptions()
		must.NotNil(t, actual)

		// The zero value is "no ordering requirement", which every backend has
		// to be able to tell apart from a key.
		test.EqOp(t, "", actual.OrderingKey)
		test.EqOp(t, "", actual.DeduplicationKey)
	})

	T.Run("with both options", func(t *testing.T) {
		t.Parallel()

		actual := NewPublishOptions(WithOrderingKey("account_123"), WithDeduplicationKey("event_456"))
		must.NotNil(t, actual)

		test.EqOp(t, "account_123", actual.OrderingKey)
		test.EqOp(t, "event_456", actual.DeduplicationKey)
	})

	T.Run("applies options in order", func(t *testing.T) {
		t.Parallel()

		actual := NewPublishOptions(WithOrderingKey("first"), WithOrderingKey("second"))
		must.NotNil(t, actual)

		test.EqOp(t, "second", actual.OrderingKey)
	})

	T.Run("skips a nil option", func(t *testing.T) {
		t.Parallel()

		actual := NewPublishOptions(nil, WithOrderingKey("account_123"))
		must.NotNil(t, actual)

		test.EqOp(t, "account_123", actual.OrderingKey)
	})
}
