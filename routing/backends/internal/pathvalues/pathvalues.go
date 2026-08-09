// Package pathvalues holds the one decode step every routing backend that
// matches on the escaped path owes its callers.
//
// A path parameter whose value carries a reserved character has to travel
// percent-escaped — "{slug}" is a single segment, so a slug of "a/b" means
// "a%2Fb" on the wire or it means something else entirely. A backend therefore
// routes on the escaped path, which leaves it holding an escaped segment where
// routing.Backend.PathValue promises a decoded value.
//
// net/http's ServeMux does this for itself, so the stdlib backend has no use
// for this package; chi, gin, and httprouter do not, and share Decode so the
// four agree on what a path value is.
package pathvalues

import "net/url"

// Decode returns the decoded form of a raw, percent-escaped path value.
//
// An invalidly escaped value comes back unchanged rather than empty. net/http
// resolves it the same way (see pathUnescape in net/http/pattern.go), and an
// empty string is not a neutral answer here: routing's binder reads "" from
// PathValue as a parameter the request did not carry, so emptying a malformed
// value would report a missing parameter for one the caller plainly sent.
//
// Exactly one decode is owed, and it is the caller that knows whether it is
// owed at all: Decode is correct only for a segment taken from the escaped path.
// A value that is itself escaped text makes the difference visible. A request
// for "a%252Fb" carries the value "a%2Fb", and net/url leaves URL.RawPath empty
// for it, because escaping the decoded path reproduces what arrived — so a
// backend that reads URL.Path in that case is already holding the answer, and
// running it through Decode would quietly turn it into "a/b". Backends that
// always match on the escaped path (gin here, and httprouter for the requests it
// routes escaped) can call Decode unconditionally; chi, which falls back to the
// decoded path, decodes only when URL.RawPath is set.
func Decode(raw string) string {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return raw
	}

	return decoded
}
