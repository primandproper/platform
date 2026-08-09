package charset

import (
	"regexp"
	"testing"
)

var (
	boolSink   bool
	stringSink string
)

func BenchmarkChecker(b *testing.B) {
	var (
		body = ASCIIAlphanumeric.Union(Bytes('_'))
		head = ASCIILetters.Union(Bytes('_'))

		identifier = New(body, WithFirst(head), WithSeparator('.', 2))
		prefix     = New(body, WithFirst(head), AllowEmpty())
		token      = New(HexDigits, WithExactLength(64))
	)

	b.Run("Valid/identifier", func(b *testing.B) {
		for b.Loop() {
			boolSink = identifier.Valid("outbox_messages")
		}
	})

	b.Run("Valid/qualified", func(b *testing.B) {
		for b.Loop() {
			boolSink = identifier.Valid("app.outbox_messages")
		}
	})

	b.Run("Valid/rejected", func(b *testing.B) {
		for b.Loop() {
			boolSink = identifier.Valid("a;DROP TABLE users;--")
		}
	})

	b.Run("Valid/prefix", func(b *testing.B) {
		for b.Loop() {
			boolSink = prefix.Valid("")
		}
	})

	b.Run("Valid/fixedWidthToken", func(b *testing.B) {
		token64 := "deadbeef00112233445566778899aabbccddeeff00112233445566778899aabb"
		for b.Loop() {
			boolSink = token.Valid(token64)
		}
	})
}

// BenchmarkCheckerVersusRegexp measures the same two rules both ways. The
// regexps are the ones the migrated call sites used to hold, so the pair is a
// before-and-after rather than a contrived comparison.
func BenchmarkCheckerVersusRegexp(b *testing.B) {
	var (
		body = ASCIIAlphanumeric.Union(Bytes('_'))
		head = ASCIILetters.Union(Bytes('_'))

		identifier = New(body, WithFirst(head), WithSeparator('.', 2))
		reIdent    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)
	)

	b.Run("charset/accepted", func(b *testing.B) {
		for b.Loop() {
			boolSink = identifier.Valid("app.outbox_messages")
		}
	})

	b.Run("regexp/accepted", func(b *testing.B) {
		for b.Loop() {
			boolSink = reIdent.MatchString("app.outbox_messages")
		}
	})

	b.Run("charset/rejected", func(b *testing.B) {
		for b.Loop() {
			boolSink = identifier.Valid("a;DROP TABLE users;--")
		}
	})

	b.Run("regexp/rejected", func(b *testing.B) {
		for b.Loop() {
			boolSink = reIdent.MatchString("a;DROP TABLE users;--")
		}
	})
}

func BenchmarkSet(b *testing.B) {
	keyBytes := AllBytes.Without(Bytes(0, '\n', '\r'))

	b.Run("ContainsAll", func(b *testing.B) {
		for b.Loop() {
			boolSink = keyBytes.ContainsAll("tenant:acme:order:01HQ8Z3V4K")
		}
	})

	b.Run("Union", func(b *testing.B) {
		for b.Loop() {
			boolSink = ASCIILetters.Union(ASCIIDigits, Bytes('_')).Contains('_')
		}
	})

	b.Run("String", func(b *testing.B) {
		for b.Loop() {
			stringSink = ASCIIAlphanumeric.String()
		}
	})
}
