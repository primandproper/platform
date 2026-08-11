package charset

import "unicode/utf8"

// TruncateUTF8 cuts s to at most limit bytes without splitting a rune, so the
// result is still valid UTF-8. A limit at or below zero yields the empty string,
// and a limit at or above len(s) returns s unchanged.
//
// The bound is in bytes because every destination these strings travel to
// measures bytes — a varchar(n) column, a metric attribute, a log field — while
// the cut is on a rune boundary because the byte a naive s[:limit] lands on may
// be the middle of one. Half a multi-byte rune is not a shorter string: it is
// one that a UTF-8 column rejects and a JSON encoder cannot represent, so the
// truncation that was meant to bound a value ends up losing it entirely.
//
// This is the one place in the package that reads runes rather than bytes; see
// the package doc for why the alphabet rules do not. A cut is not a rule about
// which characters are admissible, it is a rule about where a string may end,
// and the only answer that keeps the string decodable is a rune boundary.
func TruncateUTF8(s string, limit int) string {
	if limit <= 0 {
		return ""
	}

	if len(s) <= limit {
		return s
	}

	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}

	return s[:limit]
}
