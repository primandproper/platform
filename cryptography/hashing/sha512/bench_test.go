package sha512

import (
	"bytes"
	"fmt"
	"testing"
)

func BenchmarkSHA512Hasher_Hash(b *testing.B) {
	hasher := NewSHA512Hasher()
	for _, size := range []int{16, 256, 4096} {
		content := bytes.Repeat([]byte("a"), size)
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			for b.Loop() {
				bytesSink = hasher.Hash(content)
			}
		})
	}
}

var bytesSink []byte
