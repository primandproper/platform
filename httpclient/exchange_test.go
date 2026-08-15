package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/retry"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// claim is the response shape the exchange tests decode into, standing in for
// the typed body every consumer of this helper actually has.
type claim struct {
	ID    string `json:"id"`
	Count int    `json:"count"`
}

// request is the body shape the exchange tests encode.
type request struct {
	Worker string `json:"worker"`
}

// recordingTransport answers every request with the same response and keeps the
// last request it was handed, which is what the assertions about encoding and
// headers read.
type recordingTransport struct {
	seen *http.Request
	resp *http.Response
	err  error
	body string
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.seen = req

	if req.Body != nil && req.Body != http.NoBody {
		raw, _ := io.ReadAll(req.Body)
		t.body = string(raw)
	}

	if t.err != nil {
		return nil, t.err
	}

	return t.resp, nil
}

// exchangeClient builds a client whose only transport is the recorder, so the
// exchange is exercised over a real *http.Client without a server.
func exchangeClient(t *testing.T, transport *recordingTransport) *http.Client {
	t.Helper()

	return newClient(t, WithTransport(transport))
}

func TestJSON(t *testing.T) {
	t.Parallel()

	t.Run("decodes a typed response", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusOK, `{"id":"abc","count":3}`)}

		got, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodPost, "https://leader.example/v1/claim", &request{Worker: "w-1"})
		must.NoError(t, err)
		test.EqOp(t, claim{ID: "abc", Count: 3}, got)
	})

	t.Run("encodes the request body and describes it", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusOK, `{}`)}

		_, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodPost, "https://leader.example/v1/claim", &request{Worker: "w-1"})
		must.NoError(t, err)

		test.EqOp(t, `{"worker":"w-1"}`, transport.body)
		test.EqOp(t, "application/json", transport.seen.Header.Get("Content-Type"))
		test.EqOp(t, "application/json", transport.seen.Header.Get("Accept"))
	})

	t.Run("a nil body sends nothing and claims nothing", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusOK, `{}`)}

		_, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodGet, "https://leader.example/v1/claim", nil)
		must.NoError(t, err)

		test.EqOp(t, "", transport.body)
		test.EqOp(t, "", transport.seen.Header.Get("Content-Type"))
		test.EqOp(t, "application/json", transport.seen.Header.Get("Accept"))
	})

	// GetBody is what lets the retry transport send a second attempt. A request
	// built without it is one that silently declines to be retried, which is
	// exactly the sort of thing nobody notices until an outage.
	t.Run("a request with a body can be replayed", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusOK, `{}`)}

		_, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodPost, "https://leader.example/v1/claim", &request{Worker: "w-1"})
		must.NoError(t, err)

		must.NotNil(t, transport.seen.GetBody)

		replay, err := transport.seen.GetBody()
		must.NoError(t, err)

		raw, err := io.ReadAll(replay)
		must.NoError(t, err)
		test.EqOp(t, `{"worker":"w-1"}`, string(raw))
	})

	t.Run("a caller's headers win over the ones this sets", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusOK, `{}`)}

		_, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodPost, "https://leader.example/v1/claim", &request{Worker: "w-1"},
			WithHeader("Content-Type", "application/vnd.leader.claim+json"),
			WithHeader("Idempotency-Key", "k-1"),
			WithHeader("", "ignored"),
			nil,
		)
		must.NoError(t, err)

		test.EqOp(t, "application/vnd.leader.claim+json", transport.seen.Header.Get("Content-Type"))
		test.EqOp(t, "k-1", transport.seen.Header.Get("Idempotency-Key"))
	})

	t.Run("NoContent reads no body", func(t *testing.T) {
		t.Parallel()

		// A body the server should not have sent, and which a decode into
		// NoContent would choke on if one happened.
		transport := &recordingTransport{resp: response(http.StatusNoContent, "not json at all")}

		_, err := JSON[NoContent](t.Context(), exchangeClient(t, transport), http.MethodDelete, "https://leader.example/v1/claim/abc", nil)
		must.NoError(t, err)
	})

	t.Run("a transport error is reported, not swallowed", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("dial tcp: connection refused")
		transport := &recordingTransport{err: sentinel}

		got, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodGet, "https://leader.example/v1/claim", nil)
		must.ErrorIs(t, err, sentinel)
		test.EqOp(t, claim{}, got)
	})

	t.Run("a malformed body is a decode error and no partial value", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusOK, `{"id":"abc","count":`)}

		got, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodGet, "https://leader.example/v1/claim", nil)
		must.Error(t, err)
		test.EqOp(t, claim{}, got)
	})

	// A connection that dies mid-body is not an empty response, and decoding
	// what did arrive would turn half an answer into a whole one.
	t.Run("a body that dies mid-read is reported", func(t *testing.T) {
		t.Parallel()

		failing := response(http.StatusOK, "")
		failing.Body = io.NopCloser(io.MultiReader(
			strings.NewReader(`{"id":"abc"`),
			iotest.ErrReader(errors.New("unexpected EOF")),
		))

		got, err := JSON[claim](t.Context(), exchangeClient(t, &recordingTransport{resp: failing}), http.MethodGet, "https://leader.example/v1/claim", nil)
		must.Error(t, err)
		test.EqOp(t, claim{}, got)
	})

	t.Run("an unencodable body never reaches the wire", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusOK, `{}`)}

		_, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodPost, "https://leader.example/v1/claim", make(chan int))
		must.Error(t, err)
		must.Nil(t, transport.seen)
	})

	t.Run("a bad URL never reaches the wire", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusOK, `{}`)}

		_, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodGet, "://leader.example", nil)
		must.Error(t, err)
		must.Nil(t, transport.seen)
	})

	t.Run("no client is a reported failure rather than a panic", func(t *testing.T) {
		t.Parallel()

		_, err := JSON[claim](t.Context(), nil, http.MethodGet, "https://leader.example/v1/claim", nil)
		must.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
	})
}

func TestJSONStatusErrors(t *testing.T) {
	t.Parallel()

	t.Run("a refused status carries the request, the status, and the body", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusNotFound, "  no such claim\n")}

		got, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodGet, "https://leader.example/v1/claim/abc?token=secret", nil)
		test.EqOp(t, claim{}, got)

		statusErr, ok := errors.AsType[*StatusError](err)
		must.True(t, ok)

		test.EqOp(t, http.MethodGet, statusErr.Method)
		test.EqOp(t, "/v1/claim/abc", statusErr.Path)
		test.EqOp(t, http.StatusNotFound, statusErr.StatusCode)
		test.EqOp(t, "no such claim", statusErr.Body)
		test.False(t, statusErr.Truncated)
		test.StrContains(t, statusErr.Error(), "no such claim")
		test.StrNotContains(t, statusErr.Error(), "secret")
	})

	// A 3xx reaching this means the client did not follow it — redirects were
	// disabled, or exhausted — which is not an answer to decode. 300 is in the
	// table because it is the boundary: one off in either direction and either
	// a redirect decodes as a body or a 299 stops being an answer.
	for _, code := range []int{
		http.StatusMultipleChoices,
		http.StatusNotModified,
		http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(code)+" is refused rather than decoded", func(t *testing.T) {
			t.Parallel()

			transport := &recordingTransport{resp: response(code, `{"id":"abc"}`)}

			got, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodGet, "https://leader.example/v1/claim", nil)
			test.EqOp(t, claim{}, got)

			statusErr, ok := errors.AsType[*StatusError](err)
			must.True(t, ok)
			test.EqOp(t, code, statusErr.StatusCode)
		})
	}

	// The other side of the same boundary: the last status that is an answer.
	t.Run("a 299 is still an answer", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(299, `{"id":"abc","count":1}`)}

		got, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodGet, "https://leader.example/v1/claim", nil)
		must.NoError(t, err)
		test.EqOp(t, claim{ID: "abc", Count: 1}, got)
	})

	t.Run("an empty body leaves the message to the status alone", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusBadGateway, "")}

		_, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodGet, "https://leader.example/v1/claim", nil)

		statusErr, ok := errors.AsType[*StatusError](err)
		must.True(t, ok)
		test.EqOp(t, "", statusErr.Body)
		test.EqOp(t, "GET /v1/claim: server responded with 502 Bad Gateway", statusErr.Error())
	})

	// The bound exists so a proxy's HTML error page costs one log line rather
	// than a log budget, which means it has to bound the read and not only the
	// string.
	t.Run("a huge body is cut, marked, and not buffered", func(t *testing.T) {
		t.Parallel()

		flood := strings.Repeat("a", 4<<20)
		transport := &recordingTransport{resp: response(http.StatusInternalServerError, flood)}

		_, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodGet, "https://leader.example/v1/claim", nil)

		statusErr, ok := errors.AsType[*StatusError](err)
		must.True(t, ok)
		test.EqOp(t, DefaultErrorBodyLimit, len(statusErr.Body))
		test.True(t, statusErr.Truncated)
		test.StrHasSuffix(t, "…", statusErr.Error())
	})

	t.Run("a body that exactly fills the limit is not marked cut", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusInternalServerError, strings.Repeat("a", DefaultErrorBodyLimit))}

		_, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodGet, "https://leader.example/v1/claim", nil)

		statusErr, ok := errors.AsType[*StatusError](err)
		must.True(t, ok)
		test.EqOp(t, DefaultErrorBodyLimit, len(statusErr.Body))
		test.False(t, statusErr.Truncated)
	})

	// A cut on a byte index lands in the middle of a multi-byte rune, and half a
	// rune is not a shorter string — it is one a UTF-8 column rejects and a JSON
	// encoder cannot represent.
	t.Run("the cut lands on a rune boundary", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusInternalServerError, strings.Repeat("é", 8))}

		_, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodGet, "https://leader.example/v1/claim", nil,
			WithErrorBodyLimit(5),
		)

		statusErr, ok := errors.AsType[*StatusError](err)
		must.True(t, ok)
		test.EqOp(t, "éé", statusErr.Body)
		test.True(t, statusErr.Truncated)
	})

	t.Run("a zero limit keeps the status and none of the body", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusForbidden, "tenant 4a2f is not permitted")}

		_, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodGet, "https://leader.example/v1/claim", nil,
			WithErrorBodyLimit(0),
		)

		statusErr, ok := errors.AsType[*StatusError](err)
		must.True(t, ok)
		test.EqOp(t, "", statusErr.Body)
		test.EqOp(t, "GET /v1/claim: server responded with 403 Forbidden", statusErr.Error())
	})

	t.Run("a negative limit leaves the default in place", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{resp: response(http.StatusInternalServerError, strings.Repeat("a", 4<<10))}

		_, err := JSON[claim](t.Context(), exchangeClient(t, transport), http.MethodGet, "https://leader.example/v1/claim", nil,
			WithErrorBodyLimit(-1),
		)

		statusErr, ok := errors.AsType[*StatusError](err)
		must.True(t, ok)
		test.EqOp(t, DefaultErrorBodyLimit, len(statusErr.Body))
	})
}

// The whole point of the error carrying a status is that a caller's own retry
// loop reaches the same verdict the retry transport would have, without
// restating the rule.
func TestStatusErrorRetryClassification(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		code     int
		terminal bool
	}{
		{name: "bad request", code: http.StatusBadRequest, terminal: true},
		{name: "not found", code: http.StatusNotFound, terminal: true},
		{name: "conflict", code: http.StatusConflict, terminal: true},
		{name: "request timeout", code: http.StatusRequestTimeout, terminal: false},
		{name: "too many requests", code: http.StatusTooManyRequests, terminal: false},
		{name: "internal server error", code: http.StatusInternalServerError, terminal: false},
		{name: "service unavailable", code: http.StatusServiceUnavailable, terminal: false},
		{name: "not modified", code: http.StatusNotModified, terminal: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			statusErr := &StatusError{StatusCode: tc.code}
			test.EqOp(t, tc.terminal, errors.Is(statusErr, retry.ErrUnretryable))

			// The two readers of terminalStatus must not disagree: whatever the
			// transport's classifier says about a response is what the error
			// says about the same status.
			classified := DefaultRetryClassification(&http.Response{StatusCode: tc.code})
			test.EqOp(t, tc.terminal, errors.Is(classified, retry.ErrUnretryable))
		})
	}

	t.Run("it matches nothing else", func(t *testing.T) {
		t.Parallel()

		statusErr := &StatusError{StatusCode: http.StatusBadRequest}
		test.False(t, errors.Is(statusErr, retry.ErrExhausted))
		test.False(t, errors.Is(statusErr, platformerrors.ErrNilInputParameter))
	})
}

// The helper adds no resilience of its own, and the proof is that a client
// built with a retry policy still retries exactly as many times as it was told
// to — the exchange neither suppresses the loop nor runs a second one.
func TestJSONLeavesResilienceToTheTransports(t *testing.T) {
	t.Parallel()

	t.Run("a retrying client retries under the exchange", func(t *testing.T) {
		t.Parallel()

		attempts := 0
		base := roundTripperFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			if attempts < 3 {
				return response(http.StatusServiceUnavailable, "not yet"), nil
			}

			return response(http.StatusOK, `{"id":"abc","count":1}`), nil
		})

		client := newClient(t,
			WithTransport(base),
			WithRetryPolicy(&immediatePolicy{attempts: 4}),
		)

		got, err := JSON[claim](t.Context(), client, http.MethodGet, "https://leader.example/v1/claim", nil)
		must.NoError(t, err)
		test.EqOp(t, claim{ID: "abc", Count: 1}, got)
		test.EqOp(t, 3, attempts)
	})

	t.Run("a terminal status stops the transport's loop, and the exchange starts no other", func(t *testing.T) {
		t.Parallel()

		attempts := 0
		base := roundTripperFunc(func(*http.Request) (*http.Response, error) {
			attempts++

			return response(http.StatusBadRequest, "malformed claim"), nil
		})

		client := newClient(t,
			WithTransport(base),
			WithRetryPolicy(&immediatePolicy{attempts: 4}),
		)

		_, err := JSON[claim](t.Context(), client, http.MethodGet, "https://leader.example/v1/claim", nil)
		must.ErrorIs(t, err, retry.ErrUnretryable)
		test.EqOp(t, 1, attempts)
	})
}

// The request has to carry the caller's context, because that is what a
// cancellation and the client's own timeout travel on. A real transport reads
// it; the recorder here only proves it arrived.
func TestJSONBindsTheCallersContext(t *testing.T) {
	t.Parallel()

	type ctxKey struct{}

	ctx := context.WithValue(t.Context(), ctxKey{}, "carried")
	transport := &recordingTransport{resp: response(http.StatusOK, `{}`)}

	_, err := JSON[claim](ctx, exchangeClient(t, transport), http.MethodGet, "https://leader.example/v1/claim", nil)
	must.NoError(t, err)

	test.EqOp(t, "carried", transport.seen.Context().Value(ctxKey{}))
}
