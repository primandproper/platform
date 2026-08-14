package charset

import (
	"testing"

	"github.com/shoenig/test"
)

func TestRange(T *testing.T) {
	T.Parallel()

	T.Run("holds both endpoints and everything between", func(t *testing.T) {
		t.Parallel()

		s := Range('c', 'f')

		for _, b := range []byte{'c', 'd', 'e', 'f'} {
			test.True(t, s.Contains(b), test.Sprintf("byte %q", b))
		}
		for _, b := range []byte{'b', 'g', 0, 255} {
			test.False(t, s.Contains(b), test.Sprintf("byte %q", b))
		}
	})

	T.Run("a single-byte range holds one byte", func(t *testing.T) {
		t.Parallel()

		s := Range('x', 'x')

		test.True(t, s.Contains('x'))
		test.False(t, s.Contains('w'))
		test.False(t, s.Contains('y'))
	})

	T.Run("spans the full byte range without wrapping", func(t *testing.T) {
		t.Parallel()

		s := Range(0, 255)

		for b := range 256 {
			test.True(t, s.Contains(byte(b)), test.Sprintf("byte %d", b))
		}
	})

	T.Run("a reversed range is empty rather than a panic", func(t *testing.T) {
		t.Parallel()

		test.True(t, Range('f', 'c').Empty())
	})
}

func TestBytes(T *testing.T) {
	T.Parallel()

	T.Run("holds exactly what it was given", func(t *testing.T) {
		t.Parallel()

		s := Bytes('_', '-', '.')

		for _, b := range []byte{'_', '-', '.'} {
			test.True(t, s.Contains(b), test.Sprintf("byte %q", b))
		}
		test.False(t, s.Contains('a'))
		test.False(t, s.Contains(','))
	})

	T.Run("no bytes is the empty set", func(t *testing.T) {
		t.Parallel()

		test.True(t, Bytes().Empty())
	})

	T.Run("holds a byte no well-formed UTF-8 can produce", func(t *testing.T) {
		t.Parallel()

		// 0x80-0xFF fit in a byte and so are representable here, but they are
		// continuation and lead bytes: a Set holding one matches only input
		// that was never valid UTF-8.
		s := Bytes(0xFF)

		test.True(t, s.Contains(0xFF))
		test.False(t, s.ContainsAll("é"))
	})
}

func TestSetUnion(T *testing.T) {
	T.Parallel()

	T.Run("holds the bytes of every operand", func(t *testing.T) {
		t.Parallel()

		s := Range('a', 'c').Union(Range('x', 'z'), Bytes('_'))

		for _, b := range []byte{'a', 'b', 'c', 'x', 'y', 'z', '_'} {
			test.True(t, s.Contains(b), test.Sprintf("byte %q", b))
		}
		test.False(t, s.Contains('d'))
	})

	T.Run("leaves the receiver alone", func(t *testing.T) {
		t.Parallel()

		base := Range('a', 'c')
		_ = base.Union(Bytes('!'))

		test.False(t, base.Contains('!'))
	})

	T.Run("no operands changes nothing", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, ASCIIDigits, ASCIIDigits.Union())
	})
}

func TestSetWithout(T *testing.T) {
	T.Parallel()

	T.Run("drops the bytes of every operand", func(t *testing.T) {
		t.Parallel()

		s := VisibleASCII.Without(Bytes(':'))

		test.False(t, s.Contains(':'))
		test.True(t, s.Contains(';'))
		test.True(t, s.Contains('9'))
	})

	T.Run("leaves the receiver alone", func(t *testing.T) {
		t.Parallel()

		base := ASCIILetters
		_ = base.Without(ASCIILower)

		test.True(t, base.Contains('a'))
	})

	T.Run("removing a byte that was never there changes nothing", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, ASCIIDigits, ASCIIDigits.Without(ASCIILetters))
	})
}

func TestSetContainsAll(T *testing.T) {
	T.Parallel()

	T.Run("reports whether every byte is in the set", func(t *testing.T) {
		t.Parallel()

		s := ASCIIAlphanumeric.Union(Bytes('_'))

		test.True(t, s.ContainsAll("outbox_messages_42"))
		test.False(t, s.ContainsAll("outbox-messages"))
	})

	T.Run("an empty string vacuously satisfies any set", func(t *testing.T) {
		t.Parallel()

		test.True(t, ASCIIDigits.ContainsAll(""))
		test.True(t, Bytes().ContainsAll(""))
	})

	T.Run("rejects a multi-byte character byte by byte", func(t *testing.T) {
		t.Parallel()

		// Nothing is decoded: the two bytes of é are each out of the alphabet,
		// which is the same answer as "not a letter" without having to ask what
		// character they spell.
		test.False(t, ASCIILetters.ContainsAll("naïve"))
		test.False(t, ASCIILetters.ContainsAll("a\xffb"))
	})

	T.Run("AllBytes admits anything, valid UTF-8 or not", func(t *testing.T) {
		t.Parallel()

		test.True(t, AllBytes.ContainsAll("naïve"))
		test.True(t, AllBytes.ContainsAll("a\xffb\x00"))
	})
}

func TestSetEmpty(T *testing.T) {
	T.Parallel()

	T.Run("reports whether any byte is admitted", func(t *testing.T) {
		t.Parallel()

		test.True(t, Set{}.Empty())
		test.True(t, ASCIILetters.Without(ASCIILetters).Empty())
		test.False(t, Bytes(0).Empty())
		test.False(t, ASCIIDigits.Empty())
	})
}

func TestSetString(T *testing.T) {
	T.Parallel()

	T.Run("renders the alphabets this module restricts names to", func(t *testing.T) {
		t.Parallel()

		// Runs come out in byte order, so '_' (0x5F) lands between 'Z' and 'a'
		// rather than at the end where a person writing the class by hand would
		// have put it. Stable, and not the same string as the regexp it
		// replaces — which is why nothing derives an exported error message
		// from it.
		test.EqOp(t, "[0-9A-Za-z]", ASCIIAlphanumeric.String())
		test.EqOp(t, "[0-9A-Z_a-z]", ASCIIAlphanumeric.Union(Bytes('_')).String())
		test.EqOp(t, "[A-Z_a-z]", ASCIILetters.Union(Bytes('_')).String())
		test.EqOp(t, "[0-9A-Fa-f]", HexDigits.String())
		test.EqOp(t, "[!-~]", VisibleASCII.String())
	})

	T.Run("renders an empty set as empty brackets", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "[]", Set{}.String())
	})

	// The top of the byte range is where a run has to be closed by the bound
	// rather than by the next byte not being in the set, and it is the only
	// place the two can be told apart: every other set this package ships stops
	// short of 0xFF, so a renderer that walked one byte too far would render
	// all of them correctly.
	//
	// The other half of that bound cannot be asserted at all. Widening the run
	// scan's arithmetic instead of its comparison leaves the loop with nothing
	// to stop it once a set reaches 0xFF and wraps to a byte it also contains,
	// so the only input that separates it from the code as written does not
	// terminate. A mutation report naming the scan is reporting either an
	// equivalent mutant or a hang, never a missing assertion.
	T.Run("renders a run that reaches the last byte", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, `[\x00-\xff]`, AllBytes.String())
		test.EqOp(t, `[\xfe\xff]`, Range(0xFE, 0xFF).String())
	})

	T.Run("writes a run of two out rather than hyphenating it", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "[ab]", Range('a', 'b').String())
		test.EqOp(t, "[a-c]", Range('a', 'c').String())
	})

	T.Run("escapes what would otherwise be class syntax", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, `[\-]`, Bytes('-').String())
		test.EqOp(t, `[\\]`, Bytes('\\').String())
		test.EqOp(t, `[\]]`, Bytes(']').String())
		test.EqOp(t, `[\^]`, Bytes('^').String())
	})

	T.Run("renders bytes with no printable spelling as hex", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, `[\x00]`, Bytes(0).String())
		test.EqOp(t, `[\x20]`, Bytes(' ').String())
		test.EqOp(t, `[\x7f]`, Bytes(0x7F).String())
		test.EqOp(t, `[\xff]`, Bytes(0xFF).String())
	})
}

func TestExportedSets(T *testing.T) {
	T.Parallel()

	T.Run("are ASCII and nothing else", func(t *testing.T) {
		t.Parallel()

		for name, s := range map[string]Set{
			"ASCIILower":        ASCIILower,
			"ASCIIUpper":        ASCIIUpper,
			"ASCIILetters":      ASCIILetters,
			"ASCIIDigits":       ASCIIDigits,
			"ASCIIAlphanumeric": ASCIIAlphanumeric,
			"HexDigits":         HexDigits,
			"VisibleASCII":      VisibleASCII,
		} {
			for b := 0x80; b < 256; b++ {
				test.False(t, s.Contains(byte(b)), test.Sprintf("%s holds byte %d", name, b))
			}
		}
	})

	T.Run("VisibleASCII excludes the space and DEL", func(t *testing.T) {
		t.Parallel()

		test.False(t, VisibleASCII.Contains(' '))
		test.False(t, VisibleASCII.Contains(0x7F))
		test.True(t, VisibleASCII.Contains('!'))
		test.True(t, VisibleASCII.Contains('~'))
	})

	T.Run("HexDigits takes both cases", func(t *testing.T) {
		t.Parallel()

		test.True(t, HexDigits.ContainsAll("0123456789abcdefABCDEF"))
		test.False(t, HexDigits.Contains('g'))
		test.False(t, HexDigits.Contains('G'))
	})

	T.Run("AllBytes admits every byte", func(t *testing.T) {
		t.Parallel()

		for b := range 256 {
			test.True(t, AllBytes.Contains(byte(b)), test.Sprintf("byte %d", b))
		}
	})
}
