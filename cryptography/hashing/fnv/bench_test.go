package fnv

import (
	"bytes"
	"fmt"
	"testing"
)

func BenchmarkFNVHasher_Hash(b *testing.B) {
	for _, variant := range []struct {
		hash func([]byte) []byte
		name string
	}{
		{name: "64a", hash: NewFNV64aHasher().Hash},
		{name: "128a", hash: NewFNV128aHasher().Hash},
	} {
		b.Run(variant.name, func(b *testing.B) {
			for _, size := range []int{16, 256, 4096} {
				content := bytes.Repeat([]byte("a"), size)
				b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
					for b.Loop() {
						bytesSink = variant.hash(content)
					}
				})
			}
		})
	}
}

// BenchmarkSum64a is the allocation-free path the Hasher wraps; the delta
// against BenchmarkFNVHasher_Hash/64a is the cost of rendering the number as a
// digest.
func BenchmarkSum64a(b *testing.B) {
	for _, size := range []int{16, 256, 4096} {
		content := bytes.Repeat([]byte("a"), size)
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			for b.Loop() {
				uintSink = Sum64a(content)
			}
		})
	}
}

var (
	bytesSink []byte
	uintSink  uint64
)
