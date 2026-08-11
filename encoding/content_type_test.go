package encoding

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shoenig/test"
)

func Test_clientEncoder_ContentType(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		e := NewClientEncoder(ContentTypeJSON)

		test.NotEq(t, "", e.ContentType())
	})
}

func TestContentType_String(T *testing.T) {
	T.Parallel()

	for _, ct := range ContentTypes {
		T.Run(ct.String(), func(t *testing.T) {
			t.Parallel()

			test.NotEq(t, "", ct.String())
		})
	}

	T.Run("zero value", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", ContentType("").String())
	})
}

func TestContentType_Valid(T *testing.T) {
	T.Parallel()

	for _, ct := range ContentTypes {
		T.Run(ct.String(), func(t *testing.T) {
			t.Parallel()

			test.True(t, ct.Valid())
		})
	}

	T.Run("rejects the zero value", func(t *testing.T) {
		t.Parallel()

		test.False(t, ContentType("").Valid())
	})

	T.Run("rejects an unknown media type", func(t *testing.T) {
		t.Parallel()

		test.False(t, ContentType("application/protobuf").Valid())
	})
}

func TestParseContentType(T *testing.T) {
	T.Parallel()

	for _, ct := range ContentTypes {
		T.Run(ct.String(), func(t *testing.T) {
			t.Parallel()

			parsed, err := ParseContentType(ct.String())
			test.NoError(t, err)
			test.EqOp(t, ct, parsed)
		})
	}

	T.Run("ignores charset parameter", func(t *testing.T) {
		t.Parallel()

		parsed, err := ParseContentType("application/xml; charset=utf-8")
		test.NoError(t, err)
		test.EqOp(t, ContentTypeXML, parsed)
	})

	T.Run("is case insensitive", func(t *testing.T) {
		t.Parallel()

		parsed, err := ParseContentType("  APPLICATION/JSON  ")
		test.NoError(t, err)
		test.EqOp(t, ContentTypeJSON, parsed)
	})

	// The whole point of the value type: an unrecognized content type is an
	// error, not a silent JSON default.
	T.Run("rejects an unknown media type", func(t *testing.T) {
		t.Parallel()

		parsed, err := ParseContentType("unknown")
		test.ErrorIs(t, err, ErrUnsupportedContentType)
		test.EqOp(t, ContentType(""), parsed)
	})

	T.Run("rejects the empty string", func(t *testing.T) {
		t.Parallel()

		_, err := ParseContentType("")
		test.ErrorIs(t, err, ErrUnsupportedContentType)
	})
}

func Test_contentTypeFromRequestHeader(T *testing.T) {
	T.Parallel()

	T.Run("resolves a known header", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, ContentTypeXML, contentTypeFromRequestHeader("application/xml"))
	})

	// Inbound HTTP keeps the JSON fallback: an unlabeled body is normal.
	T.Run("falls back to JSON for a missing header", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, ContentTypeJSON, contentTypeFromRequestHeader(""))
	})

	T.Run("falls back to JSON for an unknown header", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, ContentTypeJSON, contentTypeFromRequestHeader("unknown"))
	})
}

// TestUnsupportedContentTypeNeverBecomesJSON pins the contract stated on
// ContentType: an encoding this package does not implement is reported, never
// quietly served as JSON. The dispatches used to default to JSON, so a
// hand-made ContentType round-tripped as JSON while every header, log line, and
// caller said otherwise.
func TestUnsupportedContentTypeNeverBecomesJSON(T *testing.T) {
	T.Parallel()

	const bogus ContentType = "application/nonsense"

	T.Run("client encoder refuses to marshal", func(t *testing.T) {
		t.Parallel()

		_, err := NewClientEncoder(bogus).Marshal(t.Context(), map[string]string{"a": "b"})
		test.ErrorIs(t, err, ErrUnsupportedContentType)
	})

	T.Run("client encoder refuses to unmarshal", func(t *testing.T) {
		t.Parallel()

		var dest map[string]string
		err := NewClientEncoder(bogus).Unmarshal(t.Context(), []byte(`{"a":"b"}`), &dest)
		test.ErrorIs(t, err, ErrUnsupportedContentType)
	})

	T.Run("client encoder refuses to encode to a writer", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		err := NewClientEncoder(bogus).Encode(t.Context(), &buf, map[string]string{"a": "b"})
		test.ErrorIs(t, err, ErrUnsupportedContentType)
		test.Zero(t, buf.Len())
	})

	T.Run("server decoder refuses to decode bytes", func(t *testing.T) {
		t.Parallel()

		var dest map[string]string
		err := NewServerEncoderDecoder(bogus).DecodeBytes(t.Context(), []byte(`{"a":"b"}`), &dest)
		test.ErrorIs(t, err, ErrUnsupportedContentType)
	})

	T.Run("server encoder answers 500 rather than a JSON body", func(t *testing.T) {
		t.Parallel()

		res := httptest.NewRecorder()
		NewServerEncoderDecoder(bogus).RespondWithData(t.Context(), res, map[string]string{"a": "b"})

		test.EqOp(t, http.StatusInternalServerError, res.Code)
		test.Zero(t, res.Body.Len())
	})

	// The inbound fallback is the documented exception and stays: a request may
	// legitimately arrive with no Content-Type at all.
	T.Run("an unlabeled request body still decodes as JSON", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"a":"b"}`))

		var dest map[string]string
		test.NoError(t, NewServerEncoderDecoder(ContentTypeJSON).DecodeRequest(t.Context(), req, &dest))
		test.Eq(t, map[string]string{"a": "b"}, dest)
	})
}
