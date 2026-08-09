package aes

import (
	"bytes"
	"testing"

	"github.com/shoenig/test/must"
)

var byteSink []byte

func BenchmarkCipher(b *testing.B) {
	c, err := NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	must.NoError(b, err)

	ctx := b.Context()
	plaintext := bytes.Repeat([]byte("x"), 256)

	b.Run("Seal", func(b *testing.B) {
		for b.Loop() {
			byteSink, _ = c.Seal(ctx, plaintext, nil)
		}
	})

	b.Run("Open", func(b *testing.B) {
		ciphertext, sealErr := c.Seal(ctx, plaintext, nil)
		must.NoError(b, sealErr)

		for b.Loop() {
			byteSink, _ = c.Open(ctx, ciphertext, nil)
		}
	})

	// Associated data is authenticated but not encrypted, so it should cost
	// roughly a GCM update over its length and nothing else. Measured because
	// row identity is expected on every call in the crypto-shredding design,
	// and "does binding cost anything" should be answerable.
	b.Run("SealWithAssociatedData", func(b *testing.B) {
		aad := []byte("row-0123456789abcdef")

		for b.Loop() {
			byteSink, _ = c.Seal(ctx, plaintext, aad)
		}
	})
}
