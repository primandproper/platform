package encoding

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	loggingnoop "github.com/primandproper/platform-go/v8/observability/logging/noop"
	tracingnoop "github.com/primandproper/platform-go/v8/observability/tracing/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestDecode(T *testing.T) {
	T.Parallel()

	T.Run("with nil content type", func(t *testing.T) {
		t.Parallel()

		var dest example
		test.NoError(t, Decode([]byte(`{"name":"test"}`), nil, &dest))
		test.EqOp(t, "test", dest.Name)
	})

	T.Run("with explicit content type", func(t *testing.T) {
		t.Parallel()

		var dest example
		test.NoError(t, Decode([]byte(`<example><name>test</name></example>`), ContentTypeXML, &dest))
		test.EqOp(t, "test", dest.Name)
	})

	T.Run("with TOML content type", func(t *testing.T) {
		t.Parallel()

		var dest example
		test.NoError(t, Decode([]byte(`name = "test"`+"\n"), ContentTypeTOML, &dest))
		test.EqOp(t, "test", dest.Name)
	})

	T.Run("with YAML content type", func(t *testing.T) {
		t.Parallel()

		var dest example
		test.NoError(t, Decode([]byte("name: test\n"), ContentTypeYAML, &dest))
		test.EqOp(t, "test", dest.Name)
	})

	T.Run("with invalid data", func(t *testing.T) {
		t.Parallel()

		var dest example
		test.Error(t, Decode([]byte(`{invalid`), nil, &dest))
	})
}

func TestMustEncode(T *testing.T) {
	T.Parallel()

	T.Run("with nil content type", func(t *testing.T) {
		t.Parallel()

		result := MustEncode(&example{Name: t.Name()}, nil)
		test.SliceNotEmpty(t, result)
	})

	T.Run("with explicit content type", func(t *testing.T) {
		t.Parallel()

		result := MustEncode(&example{Name: t.Name()}, ContentTypeXML)
		test.SliceNotEmpty(t, result)
	})

	T.Run("panics with un-encodable data", func(t *testing.T) {
		t.Parallel()

		defer func() {
			test.NotNil(t, recover())
		}()

		MustEncode(&broken{Name: json.Number(t.Name())}, nil)
	})
}

func TestMustDecode(T *testing.T) {
	T.Parallel()

	T.Run("with nil content type", func(t *testing.T) {
		t.Parallel()

		var dest example
		MustDecode([]byte(`{"name":"test"}`), nil, &dest)
		test.EqOp(t, "test", dest.Name)
	})

	T.Run("with explicit content type", func(t *testing.T) {
		t.Parallel()

		var dest example
		MustDecode([]byte(`<example><name>test</name></example>`), ContentTypeXML, &dest)
		test.EqOp(t, "test", dest.Name)
	})

	T.Run("with YAML content type", func(t *testing.T) {
		t.Parallel()

		var dest example
		MustDecode([]byte("name: test\n"), ContentTypeYAML, &dest)
		test.EqOp(t, "test", dest.Name)
	})

	T.Run("panics with invalid data", func(t *testing.T) {
		t.Parallel()

		defer func() {
			test.NotNil(t, recover())
		}()

		var dest example
		MustDecode([]byte(`{invalid`), nil, &dest)
	})
}

func TestMustEncodeJSON(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		result := MustEncodeJSON(&example{Name: t.Name()})
		test.SliceNotEmpty(t, result)
	})
}

func TestDecodeJSON(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		var dest example
		test.NoError(t, DecodeJSON([]byte(`{"name":"test"}`), &dest))
		test.EqOp(t, "test", dest.Name)
	})

	T.Run("with invalid data", func(t *testing.T) {
		t.Parallel()

		var dest example
		test.Error(t, DecodeJSON([]byte(`{invalid`), &dest))
	})
}

func TestMustDecodeJSON(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		var dest example
		MustDecodeJSON([]byte(`{"name":"test"}`), &dest)
		test.EqOp(t, "test", dest.Name)
	})
}

func TestMustJSONIntoReader(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		reader := MustJSONIntoReader(&example{Name: t.Name()})
		must.NotNil(t, reader)

		data, err := io.ReadAll(reader)
		must.NoError(t, err)
		test.SliceNotEmpty(t, data)
	})
}

// TestEncodePathsAgree pins the "bytes are exact" contract in the package doc:
// for JSON, every entry point must return precisely what json.Marshal returns.
//
// This is a regression guard with real history. The streaming encoders append a
// trailing newline while the marshalers do not, and this package used to reach
// for both — so MustEncodeJSON and EncodeReader disagreed by one byte on the
// same input, and whether a caller got that byte depended on which function
// they happened to pick.
func TestEncodePathsAgree(T *testing.T) {
	T.Parallel()

	T.Run("every JSON entry point equals json.Marshal", func(t *testing.T) {
		t.Parallel()

		v := &example{Name: "TestEncodePathsAgree"}

		want, err := json.Marshal(v)
		must.NoError(t, err)
		test.False(t, want[len(want)-1] == '\n', test.Sprint("json.Marshal must not end in a newline"))

		ctx := t.Context()
		clientEnc := NewClientEncoder(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), ContentTypeJSON)
		serverEnc := NewServerEncoderDecoder(loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), ContentTypeJSON)

		marshaled, err := clientEnc.Marshal(ctx, v)
		must.NoError(t, err)

		var streamed bytes.Buffer
		must.NoError(t, clientEnc.Encode(ctx, &streamed, v))

		reader, err := clientEnc.EncodeReader(ctx, v)
		must.NoError(t, err)
		read, err := io.ReadAll(reader)
		must.NoError(t, err)

		encoded, err := Encode(v, ContentTypeJSON)
		must.NoError(t, err)

		encodedJSON, err := EncodeJSON(v)
		must.NoError(t, err)

		fromReader, err := io.ReadAll(MustJSONIntoReader(v))
		must.NoError(t, err)

		for name, got := range map[string][]byte{
			"ClientEncoder.Marshal":               marshaled,
			"ClientEncoder.Encode":                streamed.Bytes(),
			"ClientEncoder.EncodeReader":          read,
			"Encode":                              encoded,
			"EncodeJSON":                          encodedJSON,
			"MustEncode":                          MustEncode(v, ContentTypeJSON),
			"MustEncodeJSON":                      MustEncodeJSON(v),
			"MustJSONIntoReader":                  fromReader,
			"ServerEncoderDecoder.MustEncode":     serverEnc.MustEncode(ctx, v),
			"ServerEncoderDecoder.MustEncodeJSON": serverEnc.MustEncodeJSON(ctx, v),
		} {
			test.Eq(t, want, got, test.Sprintf("%s disagreed with json.Marshal", name))
		}
	})

	T.Run("Encode reports an error where MustEncode would panic", func(t *testing.T) {
		t.Parallel()

		// The gap that sent callers to json.Marshal: before Encode existed,
		// every byte-returning entry point panicked, which an enqueue path
		// validating caller input cannot use.
		_, err := EncodeJSON(make(chan int))
		test.Error(t, err)
	})
}
