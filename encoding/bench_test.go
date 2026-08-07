package encoding

import (
	"testing"
	"time"
)

type benchPayload struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}

// benchSession is the five-field record the CBOR-versus-JSON size comparison
// was argued over — a string, a string, a time, a slice, and a bool.
type benchSession struct {
	CreatedAt time.Time `json:"createdAt"`
	UserID    string    `json:"userID"`
	SessionID string    `json:"sessionID"`
	Scopes    []string  `json:"scopes"`
	Admin     bool      `json:"admin"`
}

func BenchmarkServerEncoderDecoder(b *testing.B) {
	ctx := b.Context()
	ed := NewServerEncoderDecoder(ContentTypeJSON)

	in := &benchPayload{Name: "benchmark", ID: 42}
	encoded := ed.MustEncodeJSON(ctx, in)

	b.Run("EncodeJSON", func(b *testing.B) {
		for b.Loop() {
			bytesSink = ed.MustEncodeJSON(ctx, in)
		}
	})

	b.Run("DecodeBytes", func(b *testing.B) {
		for b.Loop() {
			var out benchPayload
			_ = ed.DecodeBytes(ctx, encoded, &out)
		}
	})
}

// BenchmarkContentTypes measures every content type over one payload, and
// reports each one's encoded size.
//
// encoded_bytes is a custom metric: the benchmark table only renders ns/op,
// B/op, and allocs/op, so read the size comparison from `go test -bench` output
// directly. It is what makes the "CBOR is smaller than JSON here" claim
// reproducible instead of a number someone once measured.
func BenchmarkContentTypes(b *testing.B) {
	ctx := b.Context()

	in := &benchSession{
		UserID:    "user_01HZY3K4M5N6P7Q8R9S0T1U2V3",
		SessionID: "sess_01HZY3K4M5N6P7Q8R9S0T1U2V3",
		CreatedAt: time.Date(2026, time.August, 3, 12, 30, 45, 123456789, time.UTC),
		Scopes:    []string{"read", "write", "admin"},
		Admin:     true,
	}

	for _, ct := range ContentTypes {
		enc := NewClientEncoder(ct)

		encoded, err := enc.Marshal(ctx, in)
		if err != nil {
			// TOML cannot encode every shape this payload has; skip rather
			// than fail, so adding a content type never breaks the suite.
			b.Logf("skipping %s: %v", ct, err)

			continue
		}

		b.Run(ct.String()+"/Marshal", func(b *testing.B) {
			for b.Loop() {
				bytesSink, _ = enc.Marshal(ctx, in)
			}

			b.ReportMetric(float64(len(encoded)), "encoded_bytes")
		})

		b.Run(ct.String()+"/Unmarshal", func(b *testing.B) {
			for b.Loop() {
				var out benchSession
				_ = enc.Unmarshal(ctx, encoded, &out)
			}
		})
	}
}

var bytesSink []byte
