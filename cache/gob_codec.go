package cache

import (
	"bytes"
	"encoding/gob"

	"github.com/primandproper/platform-go/v8/errors"
)

// gobCodec is the default Codec, using encoding/gob.
type gobCodec[T any] struct{}

var _ Codec[struct{}] = gobCodec[struct{}]{}

// NewGobCodec returns the default gob-backed Codec. Types must be
// gob-friendly: exported fields only, and interface-typed fields need their
// concrete types registered with gob.Register.
func NewGobCodec[T any]() Codec[T] {
	return gobCodec[T]{}
}

// Encode implements Codec via encoding/gob.
func (gobCodec[T]) Encode(value *T) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(value); err != nil {
		return nil, errors.Wrap(err, "gob-encoding value")
	}

	return buf.Bytes(), nil
}

// Decode implements Codec via encoding/gob.
func (gobCodec[T]) Decode(data []byte) (*T, error) {
	var value *T
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&value); err != nil {
		return nil, errors.Wrap(err, "gob-decoding value")
	}

	return value, nil
}
