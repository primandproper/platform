/*
Package charset states which characters a string may be made of, as a value
rather than as a loop written out again at every place that needs one.

The rules it replaces were each a few lines long and no two were identical: a
first character whose class differs from the rest, whether the empty string
counts, a length cap, one extra punctuation character, a dotted form. Those
differences are real — they follow from where each name travels — which is why
they could not be shared as a function. They can be shared as an alphabet plus
a small, closed set of ways to narrow it.

	var (
		body = charset.ASCIIAlphanumeric.Union(charset.Bytes('_'))
		head = charset.ASCIILetters.Union(charset.Bytes('_'))

		identifier = charset.New(body, charset.WithFirst(head), charset.WithSeparator('.', 2))
	)

	func ValidIdentifier(s string) bool { return identifier.Valid(s) }

What is shared is the alphabet and the scan. What is not shared is the rule:
each package still names its own Checker, documents where its names travel, and
reports its own sentinel error. A caller reading the line above learns the rule
from it, which is more than `regexp.MustCompile` offered and the reason this
package has no general-purpose predicate option — one would turn a rule stated
plainly into a knob that has to be decoded.

# Bytes, not runes

Every check here is over bytes, and every alphabet it ships is ASCII.

That is a decision about what these rules guard, not an optimization. The
strings reaching them become SQL identifiers interpolated into query text, cache
keys, idempotency keys, and metric attribute values. In those places admitting
the full Unicode letter category would let two names that render identically —
homoglyphs, or the same string in NFC and NFD — claim to be two different names,
and the failure that follows is a key collision serving one subject another's
answer. Restriction is the control; escaping is a different one, and the
packages that need it quote in addition to this rather than instead of it.

Three consequences worth stating outright:

  - Input that is not valid UTF-8 is rejected by any ASCII alphabet, because
    each of its bytes is out of range on its own terms. Nothing is decoded, so
    no invalid byte is silently folded into U+FFFD first.
  - Length bounds count bytes. Every destination these strings travel to
    measures bytes, including the identifier Postgres truncates at 63.
  - Set.Contains takes a byte, so a caller scanning runes must confirm the rune
    is ASCII before narrowing it. Set holds bytes; it does not hold characters.

A rule that genuinely admits arbitrary bytes — a denylist, rather than an
alphabet — starts from AllBytes and subtracts:

	var keyBytes = charset.AllBytes.Without(charset.Bytes(0, '\n', '\r'))

# Choosing between Set and Checker

Set.ContainsAll is the whole check for a rule that is nothing but an alphabet.
Reach for a Checker when the rule also has a distinct first character, a length
bound, or segments — and when it does not, do not: a Checker rejects the empty
string by default, and a caller who wanted only the alphabet would have to say
AllowEmpty to undo it.
*/
package charset
