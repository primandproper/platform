// Package identifier validates plain identifiers: names that travel into cache
// keys, idempotency keys, metric attribute values, and permission strings.
//
// Those destinations are the reason the rule is restriction rather than
// escaping. None of the four has a quoting convention a producing package could
// rely on, and the cache key is the one that matters most: a separator that can
// appear inside a component is a key collision, and a key collision there serves
// one subject another's answer.
//
// This is deliberately narrow. Packages whose names travel somewhere else — a
// SQL identifier interpolated into query text, a saga step name that is a
// component of a colon-separated key — have their own rule and their own
// validator, because the charset each admits follows from where the name goes
// rather than from a shared notion of "identifier". Consolidating those behind a
// configurable predicate would trade a rule each package states plainly for a
// knob each caller has to decode.
package identifier

// Valid reports whether name is a plain identifier no longer than maxLen: a
// letter or underscore, followed by letters, digits, or underscores.
//
// A leading digit is rejected so that a name is never mistakable for a number in
// the places these travel — a metric attribute value, a cache key component.
func Valid(name string, maxLen int) bool {
	if name == "" || len(name) > maxLen {
		return false
	}

	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}

	return true
}
