package elasticsearch

import (
	"testing"

	textsearch "github.com/primandproper/platform-go/v10/search/text"

	"github.com/shoenig/test"
)

func TestDeprecatedSentinels(T *testing.T) {
	T.Parallel()

	// The old names keep a caller written against v10.0.0 matching what this
	// backend returns, which holds only while they are the same values.
	// Re-declaring one with platformerrors.New would compile, and every
	// errors.Is in that caller would quietly stop matching.
	T.Run("alias the values that moved", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, ErrEmptyQueryProvided, textsearch.ErrEmptyQueryProvided)
		test.ErrorIs(t, ErrResultWindowExceeded, textsearch.ErrResultWindowExceeded)
	})
}
