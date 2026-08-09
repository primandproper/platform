package charset

import (
	"strings"
)

// Set is an immutable set of byte values, and the alphabet a Checker is built
// over. The zero Set is empty and admits nothing.
//
// It is comparable, so two alphabets assembled independently can be checked
// against each other with ==. That is the point of making it a value rather
// than a closure: an alphabet that a test can compare is one that cannot
// silently drift from the rule it is supposed to state.
type Set struct {
	bits [4]uint64
}

// Range returns the set of bytes from lo to hi inclusive. A lo above hi yields
// the empty set rather than a panic — a reversed range is a typo the
// constructing line makes visible, and every caller of this package builds its
// sets as package-level variables that a test then exercises.
func Range(lo, hi byte) Set {
	var s Set
	for b := int(lo); b <= int(hi); b++ {
		s.bits[b>>6] |= 1 << uint(b&63)
	}

	return s
}

// Bytes returns the set holding exactly the given bytes.
//
// Callers write byte literals as the rune constants they already are —
// Bytes('_', '-') — which is why a character outside ASCII is a compile error
// here rather than something this package has to report at run time: an untyped
// rune constant above 255 does not fit a byte. Values in 0x80-0xFF do fit, and
// are admitted; they cannot occur in well-formed UTF-8 text, so a Set holding
// one only ever matches input that was not valid UTF-8 to begin with.
func Bytes(bs ...byte) Set {
	var s Set
	for _, b := range bs {
		s.bits[b>>6] |= 1 << uint(b&63)
	}

	return s
}

// Union returns the set of bytes in s or in any of others.
func (s Set) Union(others ...Set) Set {
	// Indexed rather than ranged by value: a Set is four words, and copying one
	// per iteration is the whole cost of the operation.
	for i := range others {
		for w := range s.bits {
			s.bits[w] |= others[i].bits[w]
		}
	}

	return s
}

// Without returns the set of bytes in s and in none of others.
func (s Set) Without(others ...Set) Set {
	for i := range others {
		for w := range s.bits {
			s.bits[w] &^= others[i].bits[w]
		}
	}

	return s
}

// Contains reports whether b is in the set.
func (s Set) Contains(b byte) bool {
	return s.bits[b>>6]&(1<<uint(b&63)) != 0
}

// ContainsAll reports whether every byte of str is in the set. An empty string
// vacuously satisfies it; callers that mean to reject empty say so themselves,
// or use a Checker, which rejects it by default.
//
// This is the whole check for a rule that is nothing but an alphabet. A rule
// with a distinct first character, a length bound, or dotted segments wants a
// Checker.
func (s Set) ContainsAll(str string) bool {
	for i := range len(str) {
		if !s.Contains(str[i]) {
			return false
		}
	}

	return true
}

// Empty reports whether the set admits no byte at all.
func (s Set) Empty() bool {
	return s.bits == [4]uint64{}
}

// String renders the set as a bracketed character class — "[A-Za-z0-9_]" — so
// that a rule can describe itself in documentation and in error text instead of
// being restated by hand beside the code that enforces it.
//
// Bytes without a printable ASCII spelling render as \xNN, and runs come out in
// byte order, so the output is stable but not the spelling a person writing the
// class by hand would have chosen: the identifier alphabet renders as
// "[0-9A-Z_a-z]", because '_' sits between 'Z' and 'a', and VisibleASCII as
// "[!-~]". It is for reading — a test failure, a log line, a doc comment — and
// not for building a pattern or an error message a caller might match on.
func (s Set) String() string {
	var b strings.Builder

	b.WriteByte('[')

	lo := 0
	for lo < 256 {
		if !s.Contains(byte(lo)) {
			lo++

			continue
		}

		hi := lo
		for hi+1 < 256 && s.Contains(byte(hi+1)) {
			hi++
		}

		// A run of two is written out rather than hyphenated: "[ab]" says what
		// "[a-b]" says, in the same width, without inviting the reader to work
		// out what lies between.
		switch hi {
		case lo:
			b.WriteString(escape(byte(lo)))
		case lo + 1:
			b.WriteString(escape(byte(lo)))
			b.WriteString(escape(byte(hi)))
		default:
			b.WriteString(escape(byte(lo)))
			b.WriteByte('-')
			b.WriteString(escape(byte(hi)))
		}

		lo = hi + 1
	}

	b.WriteByte(']')

	return b.String()
}

// escape renders one byte as it should appear inside a character class.
func escape(b byte) string {
	switch b {
	case '\\', ']', '^', '-':
		return `\` + string(rune(b))
	}

	if b < 0x21 || b > 0x7E {
		const hex = "0123456789abcdef"

		return `\x` + string([]byte{hex[b>>4], hex[b&0xf]})
	}

	return string(rune(b))
}

// The alphabets the packages in this module actually restrict names to. Every
// one is ASCII, and deliberately so: these sets guard SQL identifiers, cache
// keys, and metric attribute values, where admitting the full Unicode letter
// category would let two names that render identically — homoglyphs, or the
// same string in NFC and NFD — claim to be different names.
var (
	// ASCIILower is a-z.
	ASCIILower = Range('a', 'z')
	// ASCIIUpper is A-Z.
	ASCIIUpper = Range('A', 'Z')
	// ASCIILetters is a-z and A-Z.
	ASCIILetters = ASCIILower.Union(ASCIIUpper)
	// ASCIIDigits is 0-9.
	ASCIIDigits = Range('0', '9')
	// ASCIIAlphanumeric is a-z, A-Z and 0-9.
	ASCIIAlphanumeric = ASCIILetters.Union(ASCIIDigits)
	// HexDigits is 0-9, a-f and A-F. Both cases: the hex-encoded tokens this
	// module receives are minted elsewhere, and rejecting one for its case
	// would refuse a value that is not wrong.
	HexDigits = ASCIIDigits.Union(Range('a', 'f'), Range('A', 'F'))
	// VisibleASCII is 0x21-0x7E: printable ASCII with the space excluded, and
	// DEL along with it. It is the alphabet for a string that becomes a key or
	// travels in a header, where a space is a separator rather than a
	// character.
	VisibleASCII = Range(0x21, 0x7E)
	// AllBytes admits every byte, including those that cannot appear in
	// well-formed UTF-8. It is the base for a rule that is stated as a
	// denylist — AllBytes.Without(Bytes(0)) — rather than as an alphabet.
	AllBytes = Range(0, 255)
)
