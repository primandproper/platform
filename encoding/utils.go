package encoding

import (
	"bytes"
	"context"
	"io"
)

func Decode(data []byte, ct *contentType, dest any) error {
	if ct == nil {
		ct = ContentTypeJSON
	}

	if err := NewServerEncoderDecoder(ct).DecodeBytes(context.Background(), data, dest); err != nil {
		return err
	}

	return nil
}

// Encode renders data in the given encoding, defaulting to JSON. It is the
// error-returning counterpart of Decode, and the entry point to reach for when
// something outside an HTTP handler needs bytes.
func Encode(data any, ct *contentType) ([]byte, error) {
	if ct == nil {
		ct = ContentTypeJSON
	}

	return NewClientEncoder(ct).
		Marshal(context.Background(), data)
}

// EncodeJSON JSON encodes a piece of data.
func EncodeJSON(data any) ([]byte, error) {
	return Encode(data, ContentTypeJSON)
}

// MustEncode encodes a given piece of data to a given encoding.
func MustEncode(data any, ct *contentType) []byte {
	out, err := Encode(data, ct)
	if err != nil {
		panic(err)
	}

	return out
}

// MustDecode encodes a given piece of data to a given encoding.
func MustDecode(data []byte, ct *contentType, dest any) {
	if ct == nil {
		ct = ContentTypeJSON
	}

	if err := Decode(data, ct, dest); err != nil {
		panic(err)
	}
}

// MustEncodeJSON JSON encodes a piece of data.
func MustEncodeJSON(data any) []byte {
	return MustEncode(data, ContentTypeJSON)
}

func DecodeJSON(data []byte, dest any) error {
	return Decode(data, ContentTypeJSON, dest)
}

// MustDecodeJSON JSON encodes a piece of data.
func MustDecodeJSON(data []byte, dest any) {
	MustDecode(data, ContentTypeJSON, dest)
}

// MustJSONIntoReader JSON encodes a piece of data.
func MustJSONIntoReader(data any) io.Reader {
	return bytes.NewReader(MustEncode(data, ContentTypeJSON))
}
