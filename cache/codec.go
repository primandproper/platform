package cache

// Codec converts values to and from the byte representation a serializing
// provider stores (redis today; the memory provider holds values directly and
// never encodes). NewCBORCodec is the default. NewGobCodec is the opt-in for
// values CBOR cannot carry — interface-typed fields, chiefly — and consumers
// with tight size or latency budgets can supply their own fixed-format Codec
// through the provider's constructor options.
//
// A Codec must be safe for concurrent use, and Decode must round-trip whatever
// Encode produced — the same value back, not the same bytes. Encodings that
// vary between calls (CBOR does not sort map keys) satisfy this; anything that
// decodes to a different value does not.
//
// Values written with one codec are unreadable through another, and entries
// already in a shared store carry no record of which codec wrote them. Changing
// a deployed cache's codec therefore means changing the provider's namespace so
// the old entries age out in their own keyspace, or flushing.
type Codec[T any] interface {
	Encode(value *T) ([]byte, error)
	Decode(data []byte) (*T, error)
}
