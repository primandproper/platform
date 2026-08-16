package algolia

import (
	"testing"

	textsearch "github.com/primandproper/platform-go/v11/search/text"

	"github.com/shoenig/test"
)

func TestDeprecatedSentinels(T *testing.T) {
	T.Parallel()

	// The old name keeps a caller written against v10.0.0 matching what this
	// backend returns, which holds only while they are the same value.
	// Re-declaring it with platformerrors.New would compile, and every
	// errors.Is in that caller would quietly stop matching.
	T.Run("aliases the value that moved", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, ErrEmptyQueryProvided, textsearch.ErrEmptyQueryProvided)
	})
}
