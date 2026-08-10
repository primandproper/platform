package charset

import (
	"strings"
	"testing"

	"github.com/shoenig/test"
)

// identBody and identHead are the alphabet most of this module's names are
// drawn from, reused across the tests below so each one states only what it is
// actually about.
var (
	identBody = ASCIIAlphanumeric.Union(Bytes('_'))
	identHead = ASCIILetters.Union(Bytes('_'))
)

func TestNew(T *testing.T) {
	T.Parallel()

	T.Run("an alphabet alone admits any non-empty string drawn from it", func(t *testing.T) {
		t.Parallel()

		c := New(identBody)

		test.True(t, c.Valid("outbox_messages"))
		test.True(t, c.Valid("1table"))
		test.True(t, c.Valid("_"))
		test.False(t, c.Valid("a-b"))
		test.False(t, c.Valid(""))
	})

	T.Run("an empty alphabet admits nothing", func(t *testing.T) {
		t.Parallel()

		// Not an error to construct: it means what it says, and the line that
		// built it is where the mistake is visible.
		c := New(Set{})

		test.False(t, c.Valid("a"))
		test.False(t, c.Valid(""))
	})

	T.Run("an empty alphabet still admits empty when told to", func(t *testing.T) {
		t.Parallel()

		c := New(Set{}, AllowEmpty())

		test.True(t, c.Valid(""))
		test.False(t, c.Valid("a"))
	})

	T.Run("skips nil options", func(t *testing.T) {
		t.Parallel()

		// A nil option is skipped wherever it lands, including as the last one,
		// so a caller building a slice of options conditionally does not have
		// to compact it first.
		opts := []Option{nil, AllowEmpty(), nil}

		test.True(t, New(identBody, opts...).Valid(""))
	})

	T.Run("rejects bytes that are not valid UTF-8, without decoding them", func(t *testing.T) {
		t.Parallel()

		c := New(identBody)

		test.False(t, c.Valid("a\xffb"))
		test.False(t, c.Valid("\xff"))
		test.False(t, c.Valid("naïve"))
	})
}

func TestWithFirst(T *testing.T) {
	T.Parallel()

	T.Run("gives the first character its own alphabet", func(t *testing.T) {
		t.Parallel()

		c := New(identBody, WithFirst(identHead))

		test.True(t, c.Valid("table1"))
		test.True(t, c.Valid("_t"))
		test.True(t, c.Valid("T"))
		test.False(t, c.Valid("1table"))
		test.False(t, c.Valid("9"))
	})

	T.Run("replaces the alphabet at that position rather than narrowing it", func(t *testing.T) {
		t.Parallel()

		// The head set holds a byte the body does not, so a rule that
		// intersected the two would reject "!a".
		c := New(ASCIILetters, WithFirst(Bytes('!')))

		test.True(t, c.Valid("!ab"))
		test.False(t, c.Valid("aab"))
		test.False(t, c.Valid("!a!"))
	})

	T.Run("last option wins", func(t *testing.T) {
		t.Parallel()

		c := New(identBody, WithFirst(ASCIIDigits), WithFirst(identHead))

		test.True(t, c.Valid("a1"))
		test.False(t, c.Valid("1a"))
	})

	T.Run("applies to a one-character string", func(t *testing.T) {
		t.Parallel()

		c := New(identBody, WithFirst(identHead))

		test.True(t, c.Valid("a"))
		test.False(t, c.Valid("1"))
	})
}

func TestAllowEmpty(T *testing.T) {
	T.Parallel()

	T.Run("admits the empty string", func(t *testing.T) {
		t.Parallel()

		c := New(identBody, WithFirst(identHead), AllowEmpty())

		test.True(t, c.Valid(""))
		test.True(t, c.Valid("audit"))
		test.False(t, c.Valid("1audit"))
	})

	T.Run("empty is rejected without it", func(t *testing.T) {
		t.Parallel()

		test.False(t, New(identBody).Valid(""))
	})
}

func TestWithMaxLength(T *testing.T) {
	T.Parallel()

	T.Run("bounds the string", func(t *testing.T) {
		t.Parallel()

		c := New(ASCIILetters, WithMaxLength(4))

		test.True(t, c.Valid("abcd"))
		test.False(t, c.Valid("abcde"))
	})

	T.Run("counts bytes rather than characters", func(t *testing.T) {
		t.Parallel()

		// Moot for an ASCII alphabet, which rejects the multi-byte character
		// anyway — so the case is made over AllBytes, where the alphabet has no
		// opinion and only the bound can answer.
		c := New(AllBytes, WithMaxLength(3))

		test.True(t, c.Valid("abc"))
		test.False(t, c.Valid("aéb"), test.Sprintf("four bytes, three characters"))
	})

	T.Run("last option wins", func(t *testing.T) {
		t.Parallel()

		c := New(ASCIILetters, WithMaxLength(2), WithMaxLength(4))

		test.True(t, c.Valid("abcd"))
	})
}

func TestWithExactLength(T *testing.T) {
	T.Parallel()

	T.Run("requires exactly that many bytes", func(t *testing.T) {
		t.Parallel()

		c := New(HexDigits, WithExactLength(4))

		test.True(t, c.Valid("dead"))
		test.False(t, c.Valid("dea"))
		test.False(t, c.Valid("deadb"))
		test.False(t, c.Valid(""))
	})

	T.Run("a later length option overrides it", func(t *testing.T) {
		t.Parallel()

		// Options apply in order, which is how a conflict resolves rather than
		// being an error to report.
		c := New(ASCIILetters, WithExactLength(4), WithMaxLength(6))

		test.True(t, c.Valid("abcd"))
		test.True(t, c.Valid("abcdef"))
		test.False(t, c.Valid("abc"), test.Sprintf("the minimum from WithExactLength still stands"))
	})

	T.Run("it overrides an earlier length option", func(t *testing.T) {
		t.Parallel()

		c := New(ASCIILetters, WithMaxLength(6), WithExactLength(4))

		test.True(t, c.Valid("abcd"))
		test.False(t, c.Valid("abcde"))
	})
}

func TestWithSeparator(T *testing.T) {
	T.Parallel()

	T.Run("reads the string as segments, each satisfying the rule", func(t *testing.T) {
		t.Parallel()

		c := New(identBody, WithFirst(identHead), WithSeparator('.', 2))

		test.True(t, c.Valid("outbox_messages"))
		test.True(t, c.Valid("app.outbox_messages"))
		test.False(t, c.Valid("app.1table"), test.Sprintf("the head rule applies to each segment"))
	})

	T.Run("bounds the segment count inclusively", func(t *testing.T) {
		t.Parallel()

		c := New(identBody, WithFirst(identHead), WithSeparator('.', 2))

		test.True(t, c.Valid("a"))
		test.True(t, c.Valid("a.b"))
		test.False(t, c.Valid("a.b.c"))
	})

	T.Run("a zero maximum leaves the count unbounded", func(t *testing.T) {
		t.Parallel()

		c := New(ASCIILower.Union(ASCIIDigits, Bytes('_')), WithFirst(ASCIILower), WithSeparator('.', 0))

		test.True(t, c.Valid("a"))
		test.True(t, c.Valid("a.b.c.d.e.f"))
		test.False(t, c.Valid("a.B"))
	})

	T.Run("rejects an empty segment", func(t *testing.T) {
		t.Parallel()

		c := New(identBody, WithFirst(identHead), WithSeparator('.', 0))

		test.False(t, c.Valid("a."), test.Sprintf("trailing"))
		test.False(t, c.Valid(".a"), test.Sprintf("leading"))
		test.False(t, c.Valid("a..b"), test.Sprintf("doubled"))
		test.False(t, c.Valid("."))
	})

	T.Run("the separator wins over the alphabet", func(t *testing.T) {
		t.Parallel()

		// '.' is in the body set here. Left in, "a.b" would read both as one
		// segment and as two; naming it a separator settles which.
		c := New(ASCIILetters.Union(Bytes('.')), WithSeparator('.', 2))

		test.True(t, c.Valid("a.b"))
		test.False(t, c.Valid("a.b.c"))
		test.False(t, c.Valid("a..b"))
	})

	T.Run("length bounds cover the whole string, separators included", func(t *testing.T) {
		t.Parallel()

		c := New(ASCIILetters, WithSeparator('.', 0), WithMaxLength(5))

		test.True(t, c.Valid("ab.cd"))
		test.False(t, c.Valid("abc.def"))
	})

	T.Run("empty is still governed by AllowEmpty", func(t *testing.T) {
		t.Parallel()

		test.False(t, New(ASCIILetters, WithSeparator('.', 0)).Valid(""))
		test.True(t, New(ASCIILetters, WithSeparator('.', 0), AllowEmpty()).Valid(""))
	})
}

func TestCheckerValid(T *testing.T) {
	T.Parallel()

	T.Run("a Checker is safe to share across goroutines", func(t *testing.T) {
		t.Parallel()

		c := New(identBody, WithFirst(identHead), WithSeparator('.', 2))

		for range 8 {
			t.Run("concurrent", func(t *testing.T) {
				t.Parallel()

				test.True(t, c.Valid("app.outbox_messages"))
				test.False(t, c.Valid("1table"))
			})
		}
	})

	T.Run("holds up on a long string", func(t *testing.T) {
		t.Parallel()

		c := New(identBody, WithFirst(identHead))

		test.True(t, c.Valid("a"+strings.Repeat("b", 4095)))
		test.False(t, c.Valid("a"+strings.Repeat("b", 4095)+"-"))
	})
}
