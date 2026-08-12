package cache

// Codec converts values to and from the byte representation a serializing
// provider stores (redis today; the memory provider holds values directly and
// never encodes). NewDefaultCodec is what a provider uses when given none.
// NewGobCodec is the opt-in for values CBOR cannot carry — interface-typed
// fields, chiefly — and consumers with tight size or latency budgets can supply
// their own fixed-format Codec through the provider's constructor options.
//
// A Codec must be safe for concurrent use, and Decode must round-trip whatever
// Encode produced — the same value back, not the same bytes. Encodings that
// vary between calls (CBOR does not sort map keys) satisfy this; anything that
// decodes to a different value does not.
//
// A cached type whose fields are all unexported has nothing for a structural
// codec to encode, and the failure is quiet: CBOR writes the empty map and
// reads it back with no error, so the cache serves a zero value as a clean hit.
// Give such a type MarshalBinary and UnmarshalBinary — every codec here honors
// encoding.BinaryMarshaler — and see authorization.PermissionSet for the shape.
//
// Values written with one codec are unreadable through another, and entries
// already in a shared store carry no record of which codec wrote them. Changing
// a deployed cache's codec therefore means changing the provider's namespace so
// the old entries age out in their own keyspace, or flushing.
type Codec[T any] interface {
	Encode(value *T) ([]byte, error)
	Decode(data []byte) (*T, error)
}

// NewDefaultCodec returns the Codec a serializing provider uses when the caller
// supplies none. That is CBOR today; it was gob before.
//
// It exists so "the default" has one spelling that moves when the default does.
// When CBOR replaced gob, every test and doc comment naming NewGobCodec kept
// passing while describing something that was no longer true — and
// authorization.PermissionSet, which carried gob methods precisely because it
// cannot be encoded structurally, became silently unencodable. A type you
// intend to cache should be round-tripped through this, not through a named
// codec, so that the next change of default fails the test instead of the
// deployment.
//
// The return type names today's default, so a caller that wrote the result into
// a Codec[T] keeps compiling and a caller that named the concrete type finds out
// at the next change rather than at the next deployment.
func NewDefaultCodec[T any]() CBORCodec[T] {
	return NewCBORCodec[T]()
}
