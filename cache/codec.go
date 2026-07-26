package cache

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
