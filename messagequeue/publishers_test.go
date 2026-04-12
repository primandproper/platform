package messagequeue

import (
	"testing"

	"github.com/shoenig/test"
)

func TestErrEmptyTopicName(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.NotNil(t, ErrEmptyTopicName)
		test.Error(t, ErrEmptyTopicName)
	})
}
