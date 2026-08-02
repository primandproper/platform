package encoding

import (
	"bytes"
	"context"
	"io"
)

// Decode parses data in the given encoding into dest.
//
// The zero ContentType means JSON, so callers with nothing to say about the
// encoding can pass it and get the obvious default.
func Decode(data []byte, ct ContentType, dest any) error {
	if ct == "" {
		ct = ContentTypeJSON
	}

	return NewServerEncoderDecoder(ct).DecodeBytes(context.Background(), data, dest)
}

// Encode renders data in the given encoding, defaulting to JSON. It is the
// error-returning counterpart of Decode, and the entry point to reach for when
// something outside an HTTP handler needs bytes.
func Encode(data any, ct ContentType) ([]byte, error) {
	if ct == "" {
		ct = ContentTypeJSON
	}

	return NewClientEncoder(ct).
		Marshal(context.Background(), data)
}

// EncodeJSON JSON encodes a piece of data.
func EncodeJSON(data any) ([]byte, error) {
	return Encode(data, ContentTypeJSON)
}

// MustEncode encodes a given piece of data to a given encoding, panicking on
// failure.
func MustEncode(data any, ct ContentType) []byte {
	out, err := Encode(data, ct)
	if err != nil {
		panic(err)
	}

	return out
}

// MustDecode decodes a given piece of data from a given encoding into dest,
// panicking on failure.
func MustDecode(data []byte, ct ContentType, dest any) {
	if err := Decode(data, ct, dest); err != nil {
		panic(err)
	}
}

// MustEncodeJSON JSON encodes a piece of data.
func MustEncodeJSON(data any) []byte {
	return MustEncode(data, ContentTypeJSON)
}

// DecodeJSON decodes JSON data into dest.
func DecodeJSON(data []byte, dest any) error {
	return Decode(data, ContentTypeJSON, dest)
}

// MustDecodeJSON decodes JSON data into dest, panicking on failure.
func MustDecodeJSON(data []byte, dest any) {
	MustDecode(data, ContentTypeJSON, dest)
}

// MustJSONIntoReader JSON encodes a piece of data.
func MustJSONIntoReader(data any) io.Reader {
	return bytes.NewReader(MustEncode(data, ContentTypeJSON))
}
