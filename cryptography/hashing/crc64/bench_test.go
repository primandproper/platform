package crc64

import (
	"bytes"
	"fmt"
	"testing"
)

func BenchmarkCRC64Hasher_Hash(b *testing.B) {
	hasher := NewCRC64ISOHasher()
	for _, size := range []int{16, 256, 4096} {
		content := bytes.Repeat([]byte("a"), size)
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			for b.Loop() {
				bytesSink = hasher.Hash(content)
			}
		})
	}
}

// BenchmarkChecksumISO is the allocation-free path the Hasher wraps; the delta
// against BenchmarkCRC64Hasher_Hash is the cost of rendering the number as a
// digest.
func BenchmarkChecksumISO(b *testing.B) {
	for _, size := range []int{16, 256, 4096} {
		content := bytes.Repeat([]byte("a"), size)
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			for b.Loop() {
				uintSink = ChecksumISO(content)
			}
		})
	}
}

var (
	bytesSink []byte
	uintSink  uint64
)
