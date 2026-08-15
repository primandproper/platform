package httpclient

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/primandproper/platform-go/v10/charset"
	"github.com/primandproper/platform-go/v10/retry"
)

// StatusError is a response an exchange would not accept: a status outside 2xx.
//
// It carries what an operator reading the log line actually needs — which
// request, what the server said, and what the server said about it — and it
// carries the status code so a caller can branch on a 404 without parsing a
// string.
//
// Match it with errors.As. It also matches retry.ErrUnretryable under errors.Is
// for the statuses DefaultRetryClassification calls terminal, which is what
// lets a caller's own retry loop stop on a 400 and keep trying a 429 without
// writing that rule a second time.
type StatusError struct {
	// Method is the request method, so a log line says what was attempted and
	// not only where.
	Method string

	// Path is the request's path. Not the full URL: the host is the caller's
	// own configuration and adds nothing to a message about the response, and a
	// query string is exactly where a token or a customer identifier ends up —
	// which is a poor thing to put in a string destined for a log.
	Path string

	// Status is the status line as the server sent it, "404 Not Found".
	Status string

	// Body is the response body, whitespace-trimmed and cut to at most
	// WithErrorBodyLimit bytes on a rune boundary. Truncated says whether the
	// cut happened.
	Body string

	// StatusCode is the response status code.
	StatusCode int

	// Truncated reports whether the server's body was longer than the limit, so
	// a reader knows the message ends because the bound was reached rather than
	// because the server had nothing more to say.
	Truncated bool
}

// newStatusError renders a refused response as an error, reading no more of its
// body than the limit allows.
//
// The bound is on the read and not merely on the string, which is the whole
// point of having one. A proxy's HTML error page runs to megabytes, and
// buffering it in order to throw all but 512 bytes of it away is the incident
// this is meant to prevent, not a smaller version of it. The cost is that the
// connection is not reusable when a body is left unread, which is a fair price
// on a path that has already failed.
func newStatusError(req *http.Request, resp *http.Response, limit int) *StatusError {
	// One byte past the limit, so a body that exactly fills it is not reported
	// as cut. The read error, if any, is discarded: the status is the finding
	// here, and "the server said 503 and then the connection died mid-body" is
	// still a 503.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1)) //nolint:errcheck // the status is the finding; a short body does not change it.

	return &StatusError{
		Method:     req.Method,
		Path:       req.URL.Path,
		Status:     resp.Status,
		Body:       charset.TruncateUTF8(strings.TrimSpace(string(raw)), limit),
		StatusCode: resp.StatusCode,
		Truncated:  len(raw) > limit,
	}
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("%s %s: server responded with %s", e.Method, e.Path, e.Status)
	}

	ellipsis := ""
	if e.Truncated {
		ellipsis = "…"
	}

	return fmt.Sprintf("%s %s: server responded with %s: %s%s", e.Method, e.Path, e.Status, e.Body, ellipsis)
}

// Is reports retry.ErrUnretryable for a status another attempt cannot improve.
//
// It answers rather than wraps, because retry.ErrUnretryable is not what caused
// this error — it is a fact about it, and one that depends on the status code
// the caller can read for itself. The rule is terminalStatus, the same one
// DefaultRetryClassification hands the retry transport, so an outer loop and an
// inner one cannot come to different conclusions about the same response.
func (e *StatusError) Is(target error) bool {
	return target == retry.ErrUnretryable && terminalStatus(e.StatusCode)
}
