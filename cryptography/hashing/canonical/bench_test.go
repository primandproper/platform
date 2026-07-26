package canonical

import (
	"fmt"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

// Canonicalization costs split three ways: encoding/json's marshal, the
// reparse through a json.Decoder, and one key sort per object. The payloads
// below move those terms independently — a flat struct barely sorts, a nested
// one sorts once per object, and the maps make key count the dominant term.

type benchAddress struct {
	Street string `json:"street"`
	City   string `json:"city"`
	Region string `json:"region"`
	Postal string `json:"postal"`
}

type benchLineItem struct {
	SKU      string  `json:"sku"`
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Quantity int     `json:"quantity"`
}

type benchOrder struct {
	Placed   time.Time       `json:"placed"`
	Ship     benchAddress    `json:"ship"`
	ID       string          `json:"id"`
	Customer string          `json:"customer"`
	Items    []benchLineItem `json:"items"`
	Total    float64         `json:"total"`
	Paid     bool            `json:"paid"`
}

type benchPayload struct {
	value any
	name  string
}

// benchPayloads builds the payload set shared by both benchmarks, so the Sum
// row minus the Marshal row for a given payload is the hasher's share of the
// cost.
func benchPayloads(b *testing.B) []benchPayload {
	b.Helper()

	items := make([]benchLineItem, 0, 8)
	for i := range 8 {
		items = append(items, benchLineItem{
			SKU:      fmt.Sprintf("SKU-%04d", i),
			Name:     "an ordinary product name",
			Price:    12.34,
			Quantity: i + 1,
		})
	}

	keyed := func(n int) map[string]int {
		m := make(map[string]int, n)
		for i := range n {
			// Zero-padded so insertion order and sorted order differ only by
			// map iteration randomness, not by key width.
			m[fmt.Sprintf("key-%04d", i)] = i
		}

		return m
	}

	payloads := []benchPayload{
		{
			name: "flat",
			value: benchAddress{
				Street: "1 Example Way",
				City:   "Springfield",
				Region: "IL",
				Postal: "62701",
			},
		},
		{
			name: "nested",
			value: benchOrder{
				Placed:   time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
				Ship:     benchAddress{Street: "1 Example Way", City: "Springfield", Region: "IL", Postal: "62701"},
				Items:    items,
				ID:       "order_01HZY0000000000000000000",
				Customer: "customer_01HZY0000000000000000000",
				Total:    98.72,
				Paid:     true,
			},
		},
		{name: "map-10", value: keyed(10)},
		{name: "map-100", value: keyed(100)},
	}

	// Fail loudly on an unencodable payload rather than quietly benchmarking
	// the error path.
	for i := range payloads {
		_, err := Marshal(payloads[i].value)
		must.NoError(b, err)
	}

	return payloads
}

func BenchmarkMarshal(b *testing.B) {
	payloads := benchPayloads(b)
	for i := range payloads {
		p := payloads[i]
		b.Run(p.name, func(b *testing.B) {
			for b.Loop() {
				bytesSink, _ = Marshal(p.value)
			}
		})
	}
}

func BenchmarkSum(b *testing.B) {
	payloads := benchPayloads(b)
	for i := range payloads {
		p := payloads[i]
		b.Run(p.name, func(b *testing.B) {
			for b.Loop() {
				strSink, _ = Sum(p.value)
			}
		})
	}
}

var (
	bytesSink []byte
	strSink   string
)
