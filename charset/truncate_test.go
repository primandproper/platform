package charset

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shoenig/test"
)

func TestTruncateUTF8(T *testing.T) {
	T.Parallel()

	T.Run("leaves a string within the limit alone", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "short", TruncateUTF8("short", 100))
		test.EqOp(t, "exact", TruncateUTF8("exact", 5))
	})

	T.Run("cuts ASCII at the limit", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "ab", TruncateUTF8("abc", 2))
	})

	T.Run("backs up to a rune boundary", func(t *testing.T) {
		t.Parallel()

		// "aé" is three bytes; a limit of 2 lands in the middle of the 'é'.
		got := TruncateUTF8("aé", 2)

		test.EqOp(t, "a", got)
		test.True(t, utf8.ValidString(got))
	})

	T.Run("yields empty when the first rune does not fit", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", TruncateUTF8("é", 1))
	})

	// The early return for a limit of zero is a shortcut rather than a branch
	// with its own behavior: falling through to the loop yields s[:0] for the
	// same input, so no assertion here can distinguish this guard from one
	// written `limit < 0`. A mutation report naming it is naming an equivalent
	// mutant.
	T.Run("a non-positive limit yields empty", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", TruncateUTF8("anything", 0))
		test.EqOp(t, "", TruncateUTF8("anything", -1))
	})

	// A string that begins mid-rune is not a string this package produces, but
	// it is one it can be handed: a value read from a column another process
	// truncated by bytes arrives with its first byte a continuation byte. The
	// backing-up loop stops at zero for that case, and nothing else stops it —
	// walking past the start would index the string at -1 and take down the
	// caller for an input it was asked to shorten.
	T.Run("a string that is all continuation bytes yields empty rather than panicking", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", TruncateUTF8("\x80\x80\x80", 2))
		test.EqOp(t, "", TruncateUTF8("\xbf\xbf", 1))
	})

	T.Run("every cut of a multi-byte string stays valid UTF-8", func(t *testing.T) {
		t.Parallel()

		s := strings.Repeat("héllo·wörld", 8)
		for limit := range len(s) + 4 {
			got := TruncateUTF8(s, limit)

			test.True(t, len(got) <= limit || limit < 0)
			test.True(t, utf8.ValidString(got))
			test.True(t, strings.HasPrefix(s, got))
		}
	})
}
