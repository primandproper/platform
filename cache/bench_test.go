package cache

import (
	"fmt"
	"strings"
	"testing"

	"github.com/shoenig/test/must"
)

// benchCodecValue is a representative cached value: a couple of scalars, a
// slice, and a map, all exported so gob can see them.
type benchCodecValue struct {
	Metadata map[string]string `json:"metadata"`
	Name     string            `json:"name"`
	Tags     []string          `json:"tags"`
	ID       uint64            `json:"id"`
	Active   bool              `json:"active"`
}

// BenchmarkGobCodec measures the default Codec across payload sizes. gob
// builds a fresh encoder and re-emits its type descriptors on every call
// (there is no per-Codec stream to amortize them against), so the fixed
// per-call cost dominates at small sizes. Consumers weighing a custom
// fixed-format Codec — the case cache.Codec's documentation calls out — are
// trading against these numbers, not against the byte-size curve.
func BenchmarkGobCodec(b *testing.B) {
	codec := NewGobCodec[benchCodecValue]()

	for _, size := range []int{16, 256, 4096} {
		value := &benchCodecValue{
			Metadata: map[string]string{"region": "us-east-1", "tier": "standard"},
			Tags:     []string{"alpha", "beta", "gamma"},
			Name:     strings.Repeat("a", size),
			ID:       1234567890,
			Active:   true,
		}

		encoded, err := codec.Encode(value)
		must.NoError(b, err)

		b.Run(fmt.Sprintf("Encode/%dB", size), func(b *testing.B) {
			for b.Loop() {
				bytesSink, _ = codec.Encode(value)
			}
		})

		b.Run(fmt.Sprintf("Decode/%dB", size), func(b *testing.B) {
			for b.Loop() {
				valueSink, _ = codec.Decode(encoded)
			}
		})
	}
}

var (
	bytesSink []byte
	valueSink *benchCodecValue
)
