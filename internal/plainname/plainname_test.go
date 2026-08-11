package plainname

import (
	"strings"
	"testing"

	"github.com/shoenig/test"
)

func TestValid(T *testing.T) {
	T.Parallel()

	const maxLen = 64

	T.Run("accepts plain identifiers", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{
			"a",
			"_",
			"advanced_search",
			"llm_tokens",
			"MixedCase",
			"trailing_digits_123",
			"_leading_underscore",
			strings.Repeat("a", maxLen),
		} {
			test.True(t, Valid(name, maxLen), test.Sprintf("name %q", name))
		}
	})

	T.Run("rejects anything that could break a key", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{
			"",                            // no name at all
			"has space",                   // separator in a metric attribute
			"has-dash",                    // not an identifier
			"has:colon",                   // a separator in several key formats
			"has.dot",                     // a separator in permission strings
			"has/slash",                   // a separator in cache keys
			"has\x00nul",                  // terminates a key early
			"1leading",                    // mistakable for a number
			"9",                           // same, at one character
			strings.Repeat("a", maxLen+1), // over the caller's ceiling
		} {
			test.False(t, Valid(name, maxLen), test.Sprintf("name %q", name))
		}
	})

	T.Run("a digit is positional rather than forbidden", func(t *testing.T) {
		t.Parallel()

		// The same character passes or fails on where it sits, which is the one
		// rule here that is not simply a charset.
		test.False(t, Valid("1a", maxLen))
		test.True(t, Valid("a1", maxLen))
	})

	T.Run("maxLen is the caller's, and zero admits nothing", func(t *testing.T) {
		t.Parallel()

		test.True(t, Valid("abc", 3))
		test.False(t, Valid("abc", 2))
		test.False(t, Valid("a", 0))
	})

	T.Run("length is counted in bytes", func(t *testing.T) {
		t.Parallel()

		// A multi-byte rune is rejected by the charset anyway, so the byte-vs-rune
		// distinction can never admit a name the charset would refuse. This pins
		// that, so a future charset widening has to decide the question again.
		test.False(t, Valid("é", maxLen))
	})
}
