package httpclient

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strconv"
	"time"
)

// CachedResponse is a stored response, and the type the cache handed to
// WithHTTPCache is parameterized on:
//
//	store, err := memory.NewInMemoryCache[httpclient.CachedResponse](time.Hour)
//
// It is exported for that reason alone. Nothing here is meant to be read or
// written by hand — the transport owns every field — but a cache is a typed
// collaborator and a caller cannot build one without naming what it holds.
//
// It encodes through whatever Codec the cache was built with. The default CBOR
// codec carries every field; a custom codec has to as well, so a fixed-format
// one written for this type must not drop the ones that look like metadata.
// FreshUntil and OriginTime in particular are what make an entry safe to serve
// rather than merely present.
type CachedResponse struct {
	// OriginTime is the response's Date header, or the instant it was received
	// when it sent none. Freshness derived from a max-age counts from here
	// rather than from arrival: a response that spent four minutes in somebody
	// else's cache before reaching this one has four fewer minutes to live, and
	// the Age this transport reports on a hit is measured from it.
	OriginTime time.Time

	// FreshUntil is the instant the entry stops being servable without asking
	// the origin. The zero value means it was never fresh — stored only because
	// it carries a validator, and therefore useful for revalidation and nothing
	// else.
	FreshUntil time.Time

	// Header is the stored response's header, minus the hop-by-hop fields that
	// describe one connection rather than one response.
	Header http.Header

	// Vary records the request-header values this entry was stored against, one
	// per field name the response's Vary header listed. A request whose values
	// differ is a miss: the entry answers a question this request did not ask.
	Vary map[string]string

	// Body is the complete response body. Responses whose bodies exceed the
	// configured cap are not stored at all, so this is never partial.
	Body []byte

	// StatusCode is the stored response's status.
	StatusCode int
}

// hopByHopHeaders are the fields RFC 9110 defines as belonging to a single
// connection rather than to the response, so storing them and replaying them at
// some later connection is meaningless at best.
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Proxy-Connection",
	"TE",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// newCachedResponse builds the entry for a response whose body has already been
// captured.
func newCachedResponse(
	req *http.Request,
	resp *http.Response,
	body []byte,
	originTime, freshUntil time.Time,
) *CachedResponse {
	return &CachedResponse{
		StatusCode: resp.StatusCode,
		Header:     storableHeader(resp.Header),
		Body:       body,
		OriginTime: originTime,
		FreshUntil: freshUntil,
		Vary:       varyValues(req, resp.Header),
	}
}

// storableHeader is the part of a response header worth keeping: everything
// except the fields that described the connection it arrived on.
func storableHeader(header http.Header) http.Header {
	stored := header.Clone()
	if stored == nil {
		return http.Header{}
	}

	for _, name := range hopByHopHeaders {
		stored.Del(name)
	}

	return stored
}

// clone returns an entry this transport may modify.
//
// It exists because cache/memory stores values directly rather than encoding
// them, so a Get hands back the very pointer a Set stored. Revalidation merges
// a 304's fields into the entry it read, and doing that to the stored pointer
// would be a write into the cache — and, with two requests for one URL in
// flight, a write into a map another goroutine is reading.
//
// Body is shared rather than copied. It is only ever read, and it is the one
// field large enough for copying it on every hit to matter.
func (e *CachedResponse) clone() *CachedResponse {
	cloned := *e
	cloned.Header = e.Header.Clone()

	if cloned.Header == nil {
		cloned.Header = http.Header{}
	}

	if e.Vary != nil {
		cloned.Vary = maps.Clone(e.Vary)
	}

	return &cloned
}

// fresh reports whether the entry can be served without asking the origin.
func (e *CachedResponse) fresh(now time.Time) bool {
	return now.Before(e.FreshUntil)
}

// validator returns the conditional-request headers this entry can be
// revalidated with, and whether it has any. Without one, a stale entry is worth
// nothing: the origin has no way to answer 304, so revalidating it costs a full
// response either way.
func (e *CachedResponse) validator() (etag, lastModified string, ok bool) {
	etag = e.Header.Get("ETag")
	lastModified = e.Header.Get("Last-Modified")

	return etag, lastModified, etag != "" || lastModified != ""
}

// hasValidator reports whether a response gives the origin a way to answer 304,
// which is what makes it worth storing even when nothing says it is fresh.
func hasValidator(header http.Header) bool {
	return header.Get("ETag") != "" || header.Get("Last-Modified") != ""
}

// matches reports whether the entry was stored against the same request-header
// values this request carries, for the fields the origin said it varies on.
//
// A mismatch is a miss rather than a wrong answer, which is the whole point:
// one variant per URL is retained, so a resource that genuinely varies loses
// hit rate instead of serving one caller's representation to another.
func (e *CachedResponse) matches(req *http.Request) bool {
	for name, want := range e.Vary {
		if req.Header.Get(name) != want {
			return false
		}
	}

	return true
}

// refresh folds a 304's headers into the entry and re-dates it, per RFC 9111's
// rule that a validated stored response adopts the validating response's header
// fields. The caller recomputes freshness afterward, from the merged header:
// directives the 304 omitted fall back to the ones the stored response carried,
// which is how a bare 304 extends a lifetime the origin stated once.
//
// Content-Length is deliberately not among the fields adopted. A 304 carries no
// body, and a server that sends one anyway is describing the body it would have
// sent, not the one this entry holds.
func (e *CachedResponse) refresh(header http.Header, origin time.Time) {
	for name, values := range storableHeader(header) {
		if http.CanonicalHeaderKey(name) == "Content-Length" {
			continue
		}

		e.Header[name] = values
	}

	e.OriginTime = origin
}

// toResponse rebuilds a response a caller can read exactly as it would have
// read the origin's.
//
// Everything it hands back is a copy. The memory cache stores values directly
// rather than encoding them, so a caller that mutated the header — and
// http.Client's redirect handling does — would otherwise be mutating the cache.
func (e *CachedResponse) toResponse(req *http.Request, now time.Time) *http.Response {
	header := e.Header.Clone()
	if header == nil {
		header = http.Header{}
	}

	// Age is what tells the caller this body did not just come off the wire, and
	// it is set rather than left alone precisely because the origin may have
	// sent one of its own: the stored value described the response's age when it
	// arrived here, and replaying it would understate the age by however long
	// the entry has been resident. It is measured from OriginTime, which already
	// folds in whatever age the response arrived carrying.
	header.Set("Age", strconv.FormatInt(int64(max(0, now.Sub(e.OriginTime))/time.Second), 10))

	return &http.Response{
		Status:        fmt.Sprintf("%d %s", e.StatusCode, http.StatusText(e.StatusCode)),
		StatusCode:    e.StatusCode,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(e.Body)),
		ContentLength: int64(len(e.Body)),
		Request:       req,
	}
}

// varyValues records the request headers named by the response's Vary, so a
// later lookup can tell whether this entry answers the same question.
func varyValues(req *http.Request, header http.Header) map[string]string {
	fields := varyFields(header)
	if len(fields) == 0 {
		return nil
	}

	values := make(map[string]string, len(fields))
	for _, name := range fields {
		values[name] = req.Header.Get(name)
	}

	return values
}
