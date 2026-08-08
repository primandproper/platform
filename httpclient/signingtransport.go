package httpclient

import (
	"bytes"
	"io"
	"net/http"

	"github.com/primandproper/platform-go/v10/cryptography/requestsigning"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability/keys"
)

// signingTransport stamps a request's signature headers immediately before it
// reaches the wire.
type signingTransport struct {
	base   http.RoundTripper
	signer requestsigning.Signer
	obs    *transportObserver
}

var _ http.RoundTripper = (*signingTransport)(nil)

// RoundTrip signs the request and sends it.
//
// The body is buffered, because a MAC over it cannot be computed any other way,
// and the buffered copy is what gets sent — so the bytes that were signed and
// the bytes that arrive are the same object rather than two reads of one
// stream.
func (t *signingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := readRequestBody(req)
	if err != nil {
		return nil, platformerrors.Wrap(err, "reading the request body to sign it")
	}

	// RoundTrip must not modify the request it is given, and a signature header
	// is a modification. Clone gives the copy a header map of its own.
	signed := req.Clone(req.Context())

	if err = t.signer.SignHeaders(req.Context(), signed.Header, body); err != nil {
		t.obs.signingFailures.Add(req.Context(), 1, requestAttrs(req))

		// Error, not debug: a client that cannot sign sends nothing at all, and
		// the failure is a key source this process could not read rather than
		// anything the far side did.
		t.obs.o11y.Logger().WithRequest(req).WithValue(keys.SignatureSchemeKey, t.signer.Scheme()).
			Error("signing the outbound request", err)

		return nil, platformerrors.Wrap(err, "signing the request")
	}

	if body != nil {
		signed.Body = io.NopCloser(bytes.NewReader(body))
		signed.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		signed.ContentLength = int64(len(body))
	}

	// Nothing is logged on the way through. Signing succeeds on every request a
	// working client makes, and a line per request — even at debug — costs a
	// logger allocation on the hot path to record that the ordinary thing
	// happened.
	return t.base.RoundTrip(signed)
}

// readRequestBody reads a request's body so it can be signed, and returns nil
// for a request that has none.
//
// It reads req.Body rather than calling req.GetBody, because those are not the
// same bytes when something above has already swapped one in: the retry
// transport hands each attempt a freshly rewound Body, and GetBody would hand
// back the original.
func readRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}

	body, err := io.ReadAll(req.Body)

	// Closed here rather than left to the base transport, which is handed a
	// fresh reader over these bytes and will never see this one. The close error
	// is dropped: the bytes are already in hand, and failing a request over a
	// complaint from a body that has been fully read helps nobody.
	_ = req.Body.Close() //nolint:errcheck // the body has been read; a close failure changes nothing

	if err != nil {
		return nil, err
	}

	return body, nil
}
