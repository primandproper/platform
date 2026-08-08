package httpclient

import (
	"net/http"
	"testing"
	"time"

	"github.com/shoenig/test"
)

func TestParseCacheControl(T *testing.T) {
	T.Parallel()

	T.Run("reads the directives this transport acts on", func(t *testing.T) {
		t.Parallel()

		cc := parseCacheControl(http.Header{"Cache-Control": []string{"public, max-age=300, s-maxage=60"}})

		test.True(t, cc.hasMaxAge)
		test.EqOp(t, 300*time.Second, cc.maxAge)
		test.True(t, cc.hasSMaxAge)
		test.EqOp(t, 60*time.Second, cc.sMaxAge)
		test.False(t, cc.noStore)
		test.False(t, cc.private)
	})

	T.Run("a directive in the second field binds as much as one in the first", func(t *testing.T) {
		t.Parallel()

		// Multiple Cache-Control fields are legal, and a cache that read only
		// the first would store what the second forbade.
		cc := parseCacheControl(http.Header{"Cache-Control": []string{"max-age=300", "no-store"}})

		test.True(t, cc.noStore)
		test.True(t, cc.hasMaxAge)
	})

	T.Run("case and spacing do not change the reading", func(t *testing.T) {
		t.Parallel()

		cc := parseCacheControl(http.Header{"Cache-Control": []string{"  NO-STORE ,  Max-Age = 30 "}})

		test.True(t, cc.noStore)
		test.True(t, cc.hasMaxAge)
		test.EqOp(t, 30*time.Second, cc.maxAge)
	})

	T.Run("the field-name form of no-cache is read as the bare form", func(t *testing.T) {
		t.Parallel()

		// This transport stores a response whole or not at all, so it cannot
		// honor "revalidate only this field". Treating it as plain no-cache is
		// the reading that cannot serve something the origin fenced off.
		cc := parseCacheControl(http.Header{"Cache-Control": []string{`no-cache="Set-Cookie"`}})

		test.True(t, cc.noCache)
	})

	T.Run("an unparseable age is no age at all", func(t *testing.T) {
		t.Parallel()

		cc := parseCacheControl(http.Header{"Cache-Control": []string{"max-age=soon"}})

		test.False(t, cc.hasMaxAge)
	})

	T.Run("an empty header states nothing", func(t *testing.T) {
		t.Parallel()

		cc := parseCacheControl(http.Header{})

		test.False(t, cc.hasMaxAge)
		test.False(t, cc.noStore)
		test.False(t, cc.noCache)
		test.False(t, cc.private)
	})
}

func TestVaryFields(T *testing.T) {
	T.Parallel()

	T.Run("canonicalizes and splits across fields", func(t *testing.T) {
		t.Parallel()

		fields := varyFields(http.Header{"Vary": []string{"accept-encoding, ACCEPT", "x-tenant"}})

		test.Eq(t, []string{"Accept-Encoding", "Accept", "X-Tenant"}, fields)
	})

	T.Run("a star swallows everything else", func(t *testing.T) {
		t.Parallel()

		// A response that varies on something outside the request headers
		// cannot be keyed, and a cache that kept the other field names would
		// think it had.
		test.Eq(t, []string{varyAny}, varyFields(http.Header{"Vary": []string{"Accept, *"}}))
	})

	T.Run("no Vary is no fields", func(t *testing.T) {
		t.Parallel()

		test.SliceEmpty(t, varyFields(http.Header{}))
	})
}

func TestOriginTime(T *testing.T) {
	T.Parallel()

	received := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)

	T.Run("the Date header is when the response left the origin", func(t *testing.T) {
		t.Parallel()

		header := http.Header{"Date": []string{received.Add(-30 * time.Second).Format(http.TimeFormat)}}

		test.EqOp(t, received.Add(-30*time.Second), originTime(header, received).UTC())
	})

	T.Run("an accumulated Age is subtracted from it", func(t *testing.T) {
		t.Parallel()

		// A response that spent two minutes in a CDN has two minutes less to
		// live here. Ignoring Age would hand every one of its remaining seconds
		// back to a response that had already spent them.
		header := http.Header{
			"Date": []string{received.Format(http.TimeFormat)},
			"Age":  []string{"120"},
		}

		test.EqOp(t, received.Add(-2*time.Minute), originTime(header, received).UTC())
	})

	T.Run("a Date from the future is a skewed server, not a prophecy", func(t *testing.T) {
		t.Parallel()

		header := http.Header{"Date": []string{received.Add(time.Hour).Format(http.TimeFormat)}}

		test.EqOp(t, received, originTime(header, received))
	})

	T.Run("no usable Date means the instant it arrived", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, received, originTime(http.Header{}, received))
		test.EqOp(t, received, originTime(http.Header{"Date": []string{"whenever"}}, received))
	})
}

func TestFreshUntil(T *testing.T) {
	T.Parallel()

	var (
		received = time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
		origin   = received.Add(-10 * time.Second)
	)

	T.Run("s-maxage outranks max-age", func(t *testing.T) {
		t.Parallel()

		transport := &cacheTransport{}
		header := http.Header{"Cache-Control": []string{"max-age=300, s-maxage=60"}}

		test.EqOp(t, origin.Add(time.Minute), transport.freshUntil(header, parseCacheControl(header), received, origin))
	})

	T.Run("max-age counts from the origin rather than from arrival", func(t *testing.T) {
		t.Parallel()

		transport := &cacheTransport{}
		header := http.Header{"Cache-Control": []string{"max-age=60"}}

		// Ten of the sixty seconds were spent before this cache saw it.
		test.EqOp(t, origin.Add(time.Minute), transport.freshUntil(header, parseCacheControl(header), received, origin))
	})

	T.Run("Expires is used when Cache-Control says nothing", func(t *testing.T) {
		t.Parallel()

		transport := &cacheTransport{}
		expires := received.Add(2 * time.Hour)
		header := http.Header{"Expires": []string{expires.Format(http.TimeFormat)}}

		test.EqOp(t, expires, transport.freshUntil(header, parseCacheControl(header), received, origin).UTC())
	})

	T.Run("the configured TTL fills the origin's silence, counting from arrival", func(t *testing.T) {
		t.Parallel()

		transport := &cacheTransport{ttl: 5 * time.Minute}

		test.EqOp(t,
			received.Add(5*time.Minute),
			transport.freshUntil(http.Header{}, cacheControl{}, received, origin),
		)
	})

	T.Run("the configured TTL loses to anything the origin said", func(t *testing.T) {
		t.Parallel()

		transport := &cacheTransport{ttl: time.Hour}
		header := http.Header{"Cache-Control": []string{"max-age=30"}}

		test.EqOp(t,
			origin.Add(30*time.Second),
			transport.freshUntil(header, parseCacheControl(header), received, origin),
		)
	})

	T.Run("no-cache is never fresh, whatever else is said", func(t *testing.T) {
		t.Parallel()

		transport := &cacheTransport{ttl: time.Hour}
		header := http.Header{"Cache-Control": []string{"no-cache, max-age=300"}}

		// Storable, and only ever servable after revalidating — which is worth
		// doing, since the 304 saves the body.
		test.True(t, transport.freshUntil(header, parseCacheControl(header), received, origin).IsZero())
	})

	T.Run("nothing said and no TTL is never fresh", func(t *testing.T) {
		t.Parallel()

		transport := &cacheTransport{}

		test.True(t, transport.freshUntil(http.Header{}, cacheControl{}, received, origin).IsZero())
	})
}

func TestMustRevalidate(T *testing.T) {
	T.Parallel()

	cases := map[string]struct {
		directive string
		want      bool
	}{
		"no-cache asks for the origin's word":          {directive: "no-cache", want: true},
		"max-age=0 is the older spelling of no-cache":  {directive: "max-age=0", want: true},
		"a real max-age is content with a fresh entry": {directive: "max-age=60", want: false},
		"no-store bypasses rather than revalidates":    {directive: "no-store", want: false},
		"an absent header asks for nothing":            {directive: "", want: false},
	}

	for name, testCase := range cases {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			req := newRequest(t.Context(), http.MethodGet, cacheURL, nil)
			if testCase.directive != "" {
				req.Header.Set("Cache-Control", testCase.directive)
			}

			test.EqOp(t, testCase.want, mustRevalidate(req))
		})
	}
}
