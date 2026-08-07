package cache

import (
	"fmt"
	"strings"
	"testing"
	"time"

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

// benchSessionValue is the shape the codec choice was actually argued over: a
// small session record, encoded one at a time. Its encoded size is reported as
// a benchmark metric so the size comparison between codecs is reproducible
// rather than a number in a ticket.
type benchSessionValue struct {
	CreatedAt time.Time `json:"createdAt"`
	UserID    string    `json:"userID"`
	SessionID string    `json:"sessionID"`
	Scopes    []string  `json:"scopes"`
	Admin     bool      `json:"admin"`
}

// BenchmarkCBORCodec measures the default Codec across payload sizes.
func BenchmarkCBORCodec(b *testing.B) {
	benchmarkCodec(b, NewCBORCodec[benchCodecValue]())
}

// BenchmarkGobCodec measures the opt-in gob Codec across the same payload
// sizes, for comparison against the default above.
//
// gob builds a fresh encoder and re-emits its type descriptors on every call
// (there is no per-Codec stream to amortize them against), so the fixed
// per-call cost dominates at small sizes — which is exactly the shape of a
// cache, and why it is no longer the default. Consumers weighing a custom
// fixed-format Codec are trading against these numbers.
func BenchmarkGobCodec(b *testing.B) {
	benchmarkCodec(b, NewGobCodec[benchCodecValue]())
}

func benchmarkCodec(b *testing.B, codec Codec[benchCodecValue]) {
	b.Helper()

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

// BenchmarkCodecSize reports the encoded size of one session record per codec.
// encoded_bytes is a custom metric: the benchmark table only renders ns/op,
// B/op, and allocs/op, so read this one from `go test -bench` output directly.
func BenchmarkCodecSize(b *testing.B) {
	value := &benchSessionValue{
		UserID:    "user_01HZY3K4M5N6P7Q8R9S0T1U2V3",
		SessionID: "sess_01HZY3K4M5N6P7Q8R9S0T1U2V3",
		CreatedAt: time.Date(2026, time.August, 3, 12, 30, 45, 123456789, time.UTC),
		Scopes:    []string{"read", "write", "admin"},
		Admin:     true,
	}

	codecs := []struct {
		codec Codec[benchSessionValue]
		name  string
	}{
		{name: "CBOR", codec: NewCBORCodec[benchSessionValue]()},
		{name: "Gob", codec: NewGobCodec[benchSessionValue]()},
	}

	for i := range codecs {
		codec := codecs[i].codec

		b.Run(codecs[i].name, func(b *testing.B) {
			var size int
			for b.Loop() {
				encoded, err := codec.Encode(value)
				if err != nil {
					b.Fatal(err)
				}

				size = len(encoded)
			}

			b.ReportMetric(float64(size), "encoded_bytes")
		})
	}
}

var (
	bytesSink []byte
	valueSink *benchCodecValue
)
