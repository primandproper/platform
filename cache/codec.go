package cache

import (
	"bytes"
	"encoding/gob"

	"github.com/primandproper/platform-go/v7/errors"
)

// Codec converts values to and from the byte representation a serializing
// provider stores (redis today; the memory provider holds values directly and
// never encodes). NewGobCodec is the default; consumers with tight size or
// latency budgets — large batch reads where per-value codec overhead is the
// tail latency — supply their own fixed-format Codec through the provider's
// constructor options.
//
// A Codec must be safe for concurrent use, and Decode must round-trip
// whatever Encode produced. Values written with one codec are unreadable
// through another, so changing a deployed cache's codec requires either a key
// change or a flush.
type Codec[T any] interface {
	Encode(value *T) ([]byte, error)
	Decode(data []byte) (*T, error)
}

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
