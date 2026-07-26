package eventcapture

import (
	"slices"
	"time"
)

// Bucket is one flushed aggregation cell: the caller's counter value for one
// key in one time window.
type Bucket[K comparable, C any] struct {
	// Start is the bucket window's start, floored to the bucket size, in UTC.
	Start time.Time
	// Counts is the folded counter value for the (key, window) cell.
	Counts C
	// Key is the caller's aggregation key.
	Key K
	// Size is the bucket window size.
	Size time.Duration
}

// bucketKey identifies one aggregation cell. The window start is unix seconds
// (floored to the bucket size) so the key stays comparable.
type bucketKey[K comparable] struct {
	key   K
	start int64
}

// Aggregator folds events into per-(key, time-bucket) counters of the
// caller's type. It is deliberately lock-free: ownership belongs to a single
// goroutine — the Recorder's flusher, via WithObserver/WithOnFlush — by
// construction. The cell map is bounded by maxKeys: once full, observations
// for cells not already present are dropped and counted in overflow.
type Aggregator[K comparable, C any] struct {
	cells    map[bucketKey[K]]*C
	keyOrder func(a, b K) int
	bucket   time.Duration
	maxKeys  int
	overflow uint64
}

// AggregatorOption configures an Aggregator.
type AggregatorOption[K comparable, C any] func(*Aggregator[K, C])

// WithKeyOrder supplies a comparison over keys (slices.SortFunc semantics) so
// Flush output is fully deterministic. Without it, buckets are ordered by
// window start only, with same-window order unspecified.
func WithKeyOrder[K comparable, C any](cmp func(a, b K) int) AggregatorOption[K, C] {
	return func(a *Aggregator[K, C]) {
		a.keyOrder = cmp
	}
}

// NewAggregator builds an Aggregator with the given window size and cell-map
// bound. A non-positive bucket defaults to one minute; a non-positive
// maxKeys is unbounded.
func NewAggregator[K comparable, C any](bucket time.Duration, maxKeys int, opts ...AggregatorOption[K, C]) *Aggregator[K, C] {
	if bucket <= 0 {
		bucket = time.Minute
	}

	a := &Aggregator[K, C]{
		bucket:  bucket,
		maxKeys: maxKeys,
		cells:   make(map[bucketKey[K]]*C),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}

	return a
}

// Observe folds one observation into its (key, window) cell: fold receives
// the cell's counter (zero-valued on first observation) to mutate. When the
// cell map is full and the cell does not already exist, the observation is
// dropped and counted in overflow.
func (a *Aggregator[K, C]) Observe(key K, at time.Time, fold func(*C)) {
	k := bucketKey[K]{key: key, start: at.Truncate(a.bucket).Unix()}

	c, ok := a.cells[k]
	if !ok {
		if a.maxKeys > 0 && len(a.cells) >= a.maxKeys {
			a.overflow++

			return
		}
		c = new(C)
		a.cells[k] = c
	}

	fold(c)
}

// Flush emits and removes completed buckets — those whose window ended at or
// before now — or every bucket when all is set (the drain path). Buckets are
// ordered by window start, then by WithKeyOrder when configured, so output is
// deterministic.
func (a *Aggregator[K, C]) Flush(now time.Time, all bool) []Bucket[K, C] {
	var out []Bucket[K, C]
	for k, c := range a.cells {
		start := time.Unix(k.start, 0).UTC()
		if !all && start.Add(a.bucket).After(now) {
			continue
		}

		out = append(out, Bucket[K, C]{
			Start:  start,
			Size:   a.bucket,
			Key:    k.key,
			Counts: *c,
		})
		delete(a.cells, k)
	}

	slices.SortFunc(out, func(x, y Bucket[K, C]) int {
		if c := x.Start.Compare(y.Start); c != 0 {
			return c
		}
		if a.keyOrder != nil {
			return a.keyOrder(x.Key, y.Key)
		}

		return 0
	})

	return out
}

// TakeOverflow returns and resets the count of observations dropped because
// the cell map was full, for periodic logging.
func (a *Aggregator[K, C]) TakeOverflow() uint64 {
	ov := a.overflow
	a.overflow = 0

	return ov
}
