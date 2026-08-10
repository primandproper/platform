package charset

// Checker answers whether a string is composed the way one rule says it must
// be. The rule is an alphabet, optionally narrowed by a distinct alphabet for
// the first character, a length bound, and a segment separator.
//
// A Checker is immutable once built and safe for concurrent use. Build one as a
// package-level variable beside the rule it states.
type Checker struct {
	body      Set
	first     Set
	minLength int
	maxLength int
	maxSegs   int
	hasFirst  bool
	separator byte
	segmented bool
}

// Option narrows the rule a Checker states. Options apply in order, so a later
// one overrides an earlier one that sets the same thing.
type Option func(*Checker)

// New builds a Checker over body, narrowed by opts.
//
// It cannot fail. Every configuration a caller can express means something:
// an empty body admits nothing, the last of two conflicting length bounds wins,
// and a separator that is also in body is removed from body. That is why there
// is no error to check and no Must variant to reach for — a rule here is a
// literal on the constructing line, not a string parsed at run time, so there
// is nothing a caller could get wrong that they could not see.
//
// The empty string is rejected unless AllowEmpty is given.
func New(body Set, opts ...Option) *Checker {
	c := &Checker{body: body, minLength: 1, maxLength: -1}

	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}

	// The separator wins over the body. A separator that is also an ordinary
	// character would make a string like "a.b" two readings at once, and the
	// caller who named a separator meant the one where it separates.
	if c.segmented {
		c.body = c.body.Without(Bytes(c.separator))
		c.first = c.first.Without(Bytes(c.separator))
	}

	return c
}

// WithFirst gives the first character of the string — and of every segment,
// when a separator is in play — its own alphabet, replacing rather than
// narrowing body for that one position.
//
// This is the rule that `^[A-Za-z_][A-Za-z0-9_]*$` states and that a plain
// alphabet cannot: a name may not start with a digit, because a name that could
// be read as a number is one that will be, somewhere downstream.
func WithFirst(first Set) Option {
	return func(c *Checker) {
		c.first = first
		c.hasFirst = true
	}
}

// AllowEmpty admits the empty string.
//
// Empty is rejected by default because for most of these rules a missing name
// is a bug rather than a value. Where it is a value — a table prefix, where
// empty means "the component's own names" — saying so explicitly is the point.
func AllowEmpty() Option {
	return func(c *Checker) { c.minLength = 0 }
}

// WithMaxLength bounds the string at n bytes.
//
// Bytes, not characters. Every destination these strings travel to — a column,
// a header, an identifier Postgres truncates at 63 — measures bytes, and a
// bound that counted characters would admit a name too long for the place it
// is going.
func WithMaxLength(n int) Option {
	return func(c *Checker) { c.maxLength = n }
}

// WithExactLength requires exactly n bytes, which is what a rule states when
// the string is a fixed-width token rather than a name — a ten-character team
// ID, a 64-character hex device token.
func WithExactLength(n int) Option {
	return func(c *Checker) {
		c.minLength = n
		c.maxLength = n
	}
}

// WithSeparator reads the string as segments joined by sep, each of which must
// satisfy the alphabet and the first-character rule on its own. A maxSegments
// of 0 leaves the count unbounded.
//
// An empty segment is rejected, so a leading, trailing, or doubled separator
// fails. Length bounds still apply to the whole string, separators included,
// because the limit they encode belongs to the string that is stored or sent.
//
// Use it only where the separator genuinely separates. Where it is an ordinary
// character of the alphabet that happens to be punctuation — the periods in an
// iOS bundle identifier, which Apple constrains as characters and not as
// structure — put it in the body set instead, or this will reject names that
// are not wrong.
func WithSeparator(sep byte, maxSegments int) Option {
	return func(c *Checker) {
		c.separator = sep
		c.segmented = true
		c.maxSegs = maxSegments
	}
}

// Valid reports whether s satisfies the rule.
func (c *Checker) Valid(s string) bool {
	if len(s) < c.minLength || (c.maxLength >= 0 && len(s) > c.maxLength) {
		return false
	}

	if s == "" {
		// Reachable only under AllowEmpty, which is the whole answer: there are
		// no characters to be wrong about and no segment to be empty.
		return true
	}

	if !c.segmented {
		return c.validSegment(s)
	}

	segments := 1
	start := 0

	for i := range len(s) {
		if s[i] != c.separator {
			continue
		}

		if !c.validSegment(s[start:i]) {
			return false
		}

		segments++
		if c.maxSegs > 0 && segments > c.maxSegs {
			return false
		}

		start = i + 1
	}

	return c.validSegment(s[start:])
}

// validSegment reports whether one run of characters — the whole string, or the
// part of it between two separators — satisfies the alphabet and the
// first-character rule. An empty run never does.
func (c *Checker) validSegment(s string) bool {
	if s == "" {
		return false
	}

	first := c.body
	if c.hasFirst {
		first = c.first
	}

	if !first.Contains(s[0]) {
		return false
	}

	return c.body.ContainsAll(s[1:])
}
