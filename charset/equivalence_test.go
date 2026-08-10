package charset

import (
	"regexp"
	"strings"
	"testing"

	"github.com/shoenig/test"
)

// The rules this package was built to replace, kept here verbatim as the
// specification each migrated call site has to keep meeting.
//
// They are copied rather than imported on purpose. Importing them would point
// this test at whatever the packages currently do — including, after the
// migration, at the Checkers below, which would leave it asserting that a thing
// equals itself. Copies go stale only if someone changes a rule, and changing a
// rule is exactly the thing this test exists to make loud.
var (
	reDialectIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)
	reTablePrefix       = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)?$`)
	reDataPrivacyKey    = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
	reMigrationName     = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	rePgvectorIdent     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	reAppleTeamID       = regexp.MustCompile(`^[A-Za-z0-9]{10}$`)
	reAppleBundleID     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)
	reAPNSDeviceToken   = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

// referenceIdempotencyKey is idempotency.ValidateKey's character rule, without
// the length bound that package applies from a runtime value.
func referenceIdempotencyKey(s string) bool {
	if s == "" {
		return false
	}

	for i := range len(s) {
		if c := s[i]; c <= ' ' || c > '~' {
			return false
		}
	}

	return true
}

// referenceStepName is saga.validStepName, with its fixed 64-byte cap.
func referenceStepName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}

	for i := range len(s) {
		if c := s[i]; c <= ' ' || c > '~' || c == ':' {
			return false
		}
	}

	return true
}

// referencePlainIdentifier is internal/identifier.Valid's character rule,
// without the maxLen its callers supply.
func referencePlainIdentifier(s string) bool {
	if s == "" {
		return false
	}

	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}

	return true
}

// referenceControlBytes is the workqueue and timers denylist, which stays as
// stdlib at its call sites but is the one rule here stated over arbitrary
// bytes — so it is worth pinning that this package can say it.
func referenceControlBytes(s string) bool {
	return !strings.ContainsAny(s, "\x00\n\r")
}

// equivalences pairs each rule with the Checker that must agree with it byte
// for byte. The Checkers here are the ones the migrated call sites use; if one
// of these drifts from its call site the differential stops proving anything,
// so each site's test names the same alphabet.
func equivalences() map[string]struct {
	want func(string) bool
	got  func(string) bool
} {
	var (
		body = ASCIIAlphanumeric.Union(Bytes('_'))
		head = ASCIILetters.Union(Bytes('_'))

		lowerBody = ASCIILower.Union(ASCIIDigits, Bytes('_'))

		dialectIdentifier = New(body, WithFirst(head), WithSeparator('.', 2))
		tablePrefix       = New(body, WithFirst(head), AllowEmpty())
		dataPrivacyKey    = New(lowerBody, WithFirst(ASCIILower), WithSeparator('.', 0))
		migrationName     = New(body)
		pgvectorIdent     = New(body, WithFirst(head))
		appleTeamID       = New(ASCIIAlphanumeric, WithExactLength(10))
		appleBundleID     = New(ASCIIAlphanumeric.Union(Bytes('.', '-')), WithFirst(ASCIIAlphanumeric))
		apnsDeviceToken   = New(HexDigits, WithExactLength(64))
		idempotencyKey    = New(VisibleASCII)
		stepName          = New(VisibleASCII.Without(Bytes(':')), WithMaxLength(64))
		plainIdentifier   = New(body, WithFirst(head))
		controlBytes      = AllBytes.Without(Bytes(0, '\n', '\r'))
	)

	return map[string]struct {
		want func(string) bool
		got  func(string) bool
	}{
		"database/dialect identifier":      {reDialectIdentifier.MatchString, dialectIdentifier.Valid},
		"audit table prefix":               {reTablePrefix.MatchString, tablePrefix.Valid},
		"dataprivacy registration key":     {reDataPrivacyKey.MatchString, dataPrivacyKey.Valid},
		"database/migrate migration name":  {reMigrationName.MatchString, migrationName.Valid},
		"pgvector safe identifier":         {rePgvectorIdent.MatchString, pgvectorIdent.Valid},
		"apple team ID":                    {reAppleTeamID.MatchString, appleTeamID.Valid},
		"apple bundle ID":                  {reAppleBundleID.MatchString, appleBundleID.Valid},
		"apns device token":                {reAPNSDeviceToken.MatchString, apnsDeviceToken.Valid},
		"idempotency key characters":       {referenceIdempotencyKey, idempotencyKey.Valid},
		"saga step name":                   {referenceStepName, stepName.Valid},
		"internal/identifier plain name":   {referencePlainIdentifier, plainIdentifier.Valid},
		"workqueue and timers control set": {referenceControlBytes, controlBytes.ContainsAll},
	}
}

func TestEquivalence_everyShortString(T *testing.T) {
	T.Parallel()

	// Every string of zero, one, and two bytes over the whole byte range:
	// 1 + 256 + 65536 inputs against each rule. Exhaustive rather than random,
	// so a disagreement is found on the first run rather than the hundredth,
	// and so a green run is a proof over that space rather than a sample of it.
	inputs := make([]string, 0, 1+256+65536)
	inputs = append(inputs, "")

	for i := range 256 {
		inputs = append(inputs, string([]byte{byte(i)}))

		for j := range 256 {
			inputs = append(inputs, string([]byte{byte(i), byte(j)}))
		}
	}

	for name, eq := range equivalences() {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, in := range inputs {
				if want, got := eq.want(in), eq.got(in); want != got {
					t.Fatalf("disagreement on %q: rule says %t, charset says %t", in, want, got)
				}
			}
		})
	}
}

func TestEquivalence_everyArrangementOfTheInterestingBytes(T *testing.T) {
	T.Parallel()

	// Two bytes cannot reach a doubled separator, a three-segment name, or a
	// digit that is legal only because something precedes it. This covers
	// every string of up to six bytes drawn from the alphabet where those
	// distinctions live — one byte from each class the rules disagree about.
	alphabet := []byte{'a', 'A', '0', '_', '.', '-', ':', ' ', 0x00, 0xFF}

	var inputs []string

	var build func(prefix []byte, depth int)
	build = func(prefix []byte, depth int) {
		inputs = append(inputs, string(prefix))
		if depth == 0 {
			return
		}

		for _, b := range alphabet {
			build(append(prefix, b), depth-1)
		}
	}
	build(nil, 5)

	for name, eq := range equivalences() {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, in := range inputs {
				if want, got := eq.want(in), eq.got(in); want != got {
					t.Fatalf("disagreement on %q: rule says %t, charset says %t", in, want, got)
				}
			}
		})
	}
}

func TestEquivalence_atTheLengthBoundaries(T *testing.T) {
	T.Parallel()

	// The fixed-width rules are unreachable above, where nothing is ten or
	// sixty-four bytes long. Walk each one across its boundary.
	T.Run("apple team ID is ten alphanumerics", func(t *testing.T) {
		t.Parallel()

		c := New(ASCIIAlphanumeric, WithExactLength(10))

		for n := range 14 {
			in := strings.Repeat("A", n)
			test.EqOp(t, reAppleTeamID.MatchString(in), c.Valid(in), test.Sprintf("%d bytes", n))
		}

		test.False(t, c.Valid("ABCD1234X-"))
		test.True(t, c.Valid("ABCD1234XY"))
	})

	T.Run("apns device token is sixty-four hex digits", func(t *testing.T) {
		t.Parallel()

		c := New(HexDigits, WithExactLength(64))

		for _, n := range []int{0, 1, 63, 64, 65, 128} {
			in := strings.Repeat("dead", n/4) + strings.Repeat("f", n%4)
			test.EqOp(t, reAPNSDeviceToken.MatchString(in), c.Valid(in), test.Sprintf("%d bytes", n))
		}

		test.False(t, c.Valid(strings.Repeat("g", 64)))
	})

	T.Run("saga step name is capped at sixty-four bytes", func(t *testing.T) {
		t.Parallel()

		c := New(VisibleASCII.Without(Bytes(':')), WithMaxLength(64))

		for _, n := range []int{0, 1, 63, 64, 65, 200} {
			in := strings.Repeat("x", n)
			test.EqOp(t, referenceStepName(in), c.Valid(in), test.Sprintf("%d bytes", n))
		}
	})

	T.Run("the unbounded rules stay unbounded", func(t *testing.T) {
		t.Parallel()

		long := "a" + strings.Repeat("b", 4096)

		test.EqOp(t, reDialectIdentifier.MatchString(long),
			New(ASCIIAlphanumeric.Union(Bytes('_')),
				WithFirst(ASCIILetters.Union(Bytes('_'))),
				WithSeparator('.', 2)).Valid(long))
	})
}
