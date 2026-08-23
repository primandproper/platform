package http

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
)

// Response is the recorded half of an HTTP exchange: enough to answer a
// duplicate request without running the handler again.
//
// Its fields are exported and its types are gob-friendly, because the record
// store serializes it.
type Response struct {
	// Header holds the headers worth replaying, already filtered to the
	// middleware's allowlist. It is not the handler's whole header map — see
	// WithReplayedHeaders for why replaying everything is wrong.
	Header http.Header
	// Body is the recorded body, empty when Truncated.
	Body []byte
	// StatusCode is the status the handler produced, normalized so a handler
	// that wrote nothing records 200 rather than 0.
	StatusCode int
	// Truncated reports that the response outgrew the configured cap and its
	// body was dropped. The status is still replayable, which is what keeps
	// the effect from repeating.
	Truncated bool
}

// recorder is an http.ResponseWriter that passes writes through to the client
// and keeps a bounded copy.
//
// It is written here rather than borrowed because the alternatives do not fit:
// chi's wrapper would couple this package to one router backend, and the
// platform's own is internal to routing/backends. Owning it also buys a header
// snapshot taken at WriteHeader, which is the moment headers are actually
// committed, rather than after the handler returns.
type recorder struct {
	http.ResponseWriter

	header http.Header
	buf    bytes.Buffer

	maxBody int
	status  int

	truncated   bool
	shortWrite  bool
	wroteHeader bool
}

// newRecorder wraps res, preserving whatever optional interfaces it implements.
//
// The wrapper must not advertise a capability the wrapped writer lacks:
// handlers feature-detect with w.(http.Flusher) and w.(http.Hijacker), and a
// blanket implementation would answer yes for every writer and break the
// detection. So the concrete type is chosen from what res actually supports.
//
// Every variant also implements Unwrap, which is how http.ResponseController
// reaches Flush, Hijack, and the deadline setters — the modern path, and the
// one that keeps working as new optional interfaces are added.
func newRecorder(res http.ResponseWriter, maxBody int) (http.ResponseWriter, *recorder) {
	rec := &recorder{ResponseWriter: res, maxBody: maxBody}

	flusher, canFlush := res.(http.Flusher)
	hijacker, canHijack := res.(http.Hijacker)

	switch {
	case canFlush && canHijack:
		return &flushHijackRecorder{recorder: rec, flusher: flusher, hijacker: hijacker}, rec
	case canFlush:
		return &flushRecorder{recorder: rec, flusher: flusher}, rec
	case canHijack:
		return &hijackRecorder{recorder: rec, hijacker: hijacker}, rec
	default:
		return rec, rec
	}
}

// WriteHeader records the status and snapshots the headers.
//
// The snapshot happens here because this is when the headers go on the wire. A
// handler that mutates them afterwards changes nothing the client sees, and
// recording those late edits would make a replay differ from the original.
func (r *recorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}

	r.wroteHeader = true
	r.status = status
	r.header = r.ResponseWriter.Header().Clone()

	r.ResponseWriter.WriteHeader(status)
}

// Write passes bytes through and keeps a copy up to the cap.
//
// The client is served first and unconditionally. Recording is best-effort by
// comparison: a response that cannot be recorded is still a response the client
// asked for, and withholding it to protect the record would trade a certain
// failure for a possible duplicate.
func (r *recorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}

	n, err := r.ResponseWriter.Write(b)

	if n < len(b) {
		// The connection went away mid-response. Whatever was recorded is a
		// prefix of a response the client never fully received, so it must not
		// be replayed as if it were complete.
		r.shortWrite = true
	}

	if !r.truncated {
		if r.maxBody > 0 && r.buf.Len()+n > r.maxBody {
			r.truncated = true
			r.buf.Reset()
		} else {
			r.buf.Write(b[:n])
		}
	}

	return n, err
}

// Unwrap exposes the wrapped writer to http.ResponseController.
func (r *recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// response assembles what should be recorded, filtered to the replay
// allowlist.
//
// A short write yields no body: the bytes on the wire and the bytes in the
// buffer disagree, and a replay that served the buffer would answer a later
// request with something no earlier request ever received in full.
func (r *recorder) response(allowed []string) *Response {
	status := r.status
	if status == 0 {
		// The handler never wrote. net/http sends 200, so that is what
		// happened, and it is what a replay must reproduce.
		status = http.StatusOK
	}

	out := &Response{
		Header:     filterHeader(r.header, allowed),
		StatusCode: status,
		Truncated:  r.truncated || r.shortWrite,
	}

	if !out.Truncated {
		out.Body = bytes.Clone(r.buf.Bytes())
	}

	return out
}

// filterHeader copies only the allowed headers.
func filterHeader(header http.Header, allowed []string) http.Header {
	out := make(http.Header, len(allowed))
	if header == nil {
		return out
	}

	for _, name := range allowed {
		if values := header.Values(name); len(values) > 0 {
			out[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
		}
	}

	return out
}

// writeResponse replays a recorded response, returning any write failure so
// the caller can record that the replay did not fully land.
func writeResponse(res http.ResponseWriter, recorded *Response, replayHeader string) error {
	header := res.Header()
	for name, values := range recorded.Header {
		for _, value := range values {
			header.Add(name, value)
		}
	}

	if replayHeader != "" {
		header.Set(replayHeader, "true")
	}

	if recorded.Truncated {
		// Saying so is the honest move: the status is real, the missing body
		// is not the handler's answer, and a client told neither would assume
		// an empty response was intended.
		header.Set(BodyOmittedHeader, "true")
	}

	res.WriteHeader(recorded.StatusCode)

	if len(recorded.Body) == 0 {
		return nil
	}

	// The body is not attacker-controlled input being echoed: it is the exact
	// response this service's own handler produced for this same request and
	// already sent to a client once. Replaying it verbatim is the point, and
	// the Content-Type replayed alongside it comes from the allowlist rather
	// than from the request.
	if _, err := res.Write(recorded.Body); err != nil {
		return err
	}

	return nil
}

// The variants below exist only to carry optional interfaces through. Each
// embeds *recorder for ResponseWriter and Unwrap, and forwards the one or two
// methods it is named for.
//
// io.ReaderFrom is deliberately absent from all of them: implementing ReadFrom
// would let io.Copy hand bytes straight to the underlying writer and bypass
// Write, so the response would reach the client unrecorded. Without it io.Copy
// falls back to Write, which is exactly what this needs.

type flushRecorder struct {
	*recorder
	flusher http.Flusher
}

func (r *flushRecorder) Flush() { r.flusher.Flush() }

type hijackRecorder struct {
	*recorder
	hijacker http.Hijacker
}

func (r *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) { return r.hijacker.Hijack() }

type flushHijackRecorder struct {
	*recorder
	flusher  http.Flusher
	hijacker http.Hijacker
}

func (r *flushHijackRecorder) Flush() { r.flusher.Flush() }

func (r *flushHijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return r.hijacker.Hijack()
}
