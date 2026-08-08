package httpclient

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// cacheableMethods are the methods this transport will cache. Both are safe:
// sending one twice cannot have an effect the first did not, which is what
// makes answering one from a stored copy indistinguishable from asking again.
//
// It is fixed rather than an option. Widening it is not a tuning decision — a
// cached POST is a request the server never saw, and no configuration makes
// that acceptable.
var cacheableMethods = []string{http.MethodGet, http.MethodHead}

// cacheableStatuses are the statuses worth storing: RFC 9110's heuristically
// cacheable set, minus the ones whose bodies say more about the request that
// provoked them than about the resource.
//
// 404 and 410 are in it deliberately. A missing resource is an answer, and
// re-asking for it on every call is the pattern this transport exists to stop.
var cacheableStatuses = []int{
	http.StatusOK,
	http.StatusNonAuthoritativeInfo,
	http.StatusNoContent,
	http.StatusMultipleChoices,
	http.StatusMovedPermanently,
	http.StatusPermanentRedirect,
	http.StatusNotFound,
	http.StatusGone,
}

// conditionalRequestHeaders are the fields that mean the caller is running its
// own conditional request. A request carrying one is passed straight through:
// this transport would otherwise have to reason about whose validator a 304
// answered, and the caller's precondition is not ours to satisfy from a stored
// copy.
var conditionalRequestHeaders = []string{
	"If-Match",
	"If-Modified-Since",
	"If-None-Match",
	"If-Range",
	"If-Unmodified-Since",
	"Range",
}

// cacheControl is the subset of a Cache-Control header this transport acts on.
// Directives it does not name are ignored rather than guessed at.
type cacheControl struct {
	maxAge     time.Duration
	sMaxAge    time.Duration
	hasMaxAge  bool
	hasSMaxAge bool
	noStore    bool
	noCache    bool
	private    bool
}

// parseCacheControl reads every Cache-Control field on a header. There may be
// more than one, and a directive in the second is as binding as one in the
// first.
func parseCacheControl(header http.Header) cacheControl {
	var cc cacheControl

	for _, field := range header.Values("Cache-Control") {
		for directive := range strings.SplitSeq(field, ",") {
			name, value, _ := strings.Cut(strings.TrimSpace(directive), "=")
			value = strings.Trim(strings.TrimSpace(value), `"`)

			switch strings.ToLower(strings.TrimSpace(name)) {
			case "no-store":
				cc.noStore = true
			case "no-cache":
				// The field-name form (no-cache="Set-Cookie") licenses storing
				// the rest, which this transport cannot express: it stores a
				// response whole or not at all. Treated as the bare form, which
				// is the conservative reading.
				cc.noCache = true
			case "private":
				cc.private = true
			case "max-age":
				if seconds, err := strconv.Atoi(value); err == nil {
					cc.maxAge, cc.hasMaxAge = time.Duration(seconds)*time.Second, true
				}
			case "s-maxage":
				if seconds, err := strconv.Atoi(value); err == nil {
					cc.sMaxAge, cc.hasSMaxAge = time.Duration(seconds)*time.Second, true
				}
			}
		}
	}

	return cc
}

// varyFields returns the canonicalized request-header names a response says it
// varies on. A Vary of "*" yields the sentinel varyAny, which no request can
// match and which therefore makes the response unstorable.
func varyFields(header http.Header) []string {
	var fields []string

	for _, field := range header.Values("Vary") {
		for name := range strings.SplitSeq(field, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}

			if name == varyAny {
				return []string{varyAny}
			}

			fields = append(fields, http.CanonicalHeaderKey(name))
		}
	}

	return fields
}

// varyAny is the Vary value meaning the response depends on something outside
// the request headers, which is to say on something a cache cannot key on.
const varyAny = "*"

// cacheableRequest reports whether this request may be answered from, or stored
// into, the cache.
func (t *cacheTransport) cacheableRequest(req *http.Request) bool {
	if !slices.Contains(cacheableMethods, req.Method) {
		return false
	}

	for _, name := range conditionalRequestHeaders {
		if req.Header.Get(name) != "" {
			return false
		}
	}

	// An authenticated request is uncacheable unless the caller has said
	// otherwise, and even then its credential is part of the key — see
	// WithCacheAuthorized. A shared cache handing one tenant's response to
	// another is the failure this guards.
	if req.Header.Get("Authorization") != "" && !t.authorized {
		return false
	}

	return !parseCacheControl(req.Header).noStore
}

// mustRevalidate reports whether the caller has asked for the origin's word
// even when a fresh entry is in hand. Both spellings of that request are
// honored: no-cache, and the max-age=0 that older clients send for it.
func mustRevalidate(req *http.Request) bool {
	cc := parseCacheControl(req.Header)

	return cc.noCache || (cc.hasMaxAge && cc.maxAge <= 0)
}

// storable reports whether a response may be written to the cache at all,
// before any question of how long it stays fresh.
func (t *cacheTransport) storable(resp *http.Response, cc cacheControl) bool {
	if !slices.Contains(cacheableStatuses, resp.StatusCode) {
		return false
	}

	if cc.noStore || cc.private {
		return false
	}

	// A response that varies on something unstateable cannot be keyed, so there
	// is no entry that would be correct to write.
	if slices.Contains(varyFields(resp.Header), varyAny) {
		return false
	}

	// Set-Cookie is refused for the same reason Authorization is: the cache may
	// be a shared Redis, and a stored cookie is a credential this transport
	// would hand to whoever asks next. A response that means it to be cached
	// says so with public and does not set cookies alongside it.
	return len(resp.Header.Values("Set-Cookie")) == 0
}

// originTime is when the response left the origin: its Date header, less
// whatever Age some cache in front of this one already accumulated, or the
// instant it was received when it sent no usable Date.
//
// Both corrections matter for the same reason. A max-age counts from the origin
// and not from arrival, so a response that spent four of its five permitted
// minutes in a CDN has one minute left here, not five. Ignoring Date would
// grant it five; ignoring Age would grant it five for the many origins that sit
// behind a cache of their own.
//
// A Date ahead of this clock is a skewed server rather than a prophecy, and
// honoring it would extend every max-age by the size of the skew.
func originTime(header http.Header, receivedAt time.Time) time.Time {
	date, err := http.ParseTime(header.Get("Date"))
	if err != nil || date.After(receivedAt) {
		return receivedAt
	}

	if seconds, ageErr := strconv.Atoi(strings.TrimSpace(header.Get("Age"))); ageErr == nil && seconds > 0 {
		date = date.Add(-time.Duration(seconds) * time.Second)
	}

	return date
}

// freshUntil is when a stored response stops being servable without asking the
// origin. The zero time means it never was.
//
// The order is RFC 9111's, with one addition at the end: s-maxage, then
// max-age, then Expires, and only then the TTL this client was configured with.
// The configured TTL is last because it is a guess made by the caller, and any
// statement the origin made about its own resource beats a guess. It counts
// from receipt rather than from the origin's Date, because a caller naming a
// TTL means "hold this for five minutes", not "hold it for whatever is left of
// five minutes".
func (t *cacheTransport) freshUntil(header http.Header, cc cacheControl, received, origin time.Time) time.Time {
	switch {
	case cc.noCache:
		// Storable, and never servable without revalidating. That is what
		// no-cache asks for, and it is still worth storing: the 304 that
		// follows saves the body.
		return time.Time{}
	case cc.hasSMaxAge:
		return origin.Add(cc.sMaxAge)
	case cc.hasMaxAge:
		return origin.Add(cc.maxAge)
	default:
	}

	if expires, err := http.ParseTime(header.Get("Expires")); err == nil {
		return expires
	}

	if t.ttl > 0 {
		return received.Add(t.ttl)
	}

	return time.Time{}
}
