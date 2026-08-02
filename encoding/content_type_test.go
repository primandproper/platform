package encoding

import (
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
