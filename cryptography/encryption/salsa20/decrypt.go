package salsa20

import (
	"context"
	"encoding/base64"

	"github.com/primandproper/platform-go/v10/cryptography/encryption"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/keys"

	"golang.org/x/crypto/nacl/secretbox"
)

func (e *salsa20Impl) Decrypt(ctx context.Context, content string) (string, error) {
	_, op := e.o11y.Begin(ctx, observability.WithValue(keys.LengthKey, len(content)))
	defer op.End()

	raw, err := base64.URLEncoding.DecodeString(content)
	if err != nil {
		return "", op.Error(err, "decoding ciphered content")
	}

	if len(raw) < nonceSize {
		return "", op.Error(encryption.ErrMalformedCiphertext, "ciphertext too short for nonce")
	}

	var nonce [nonceSize]byte
	copy(nonce[:], raw[:nonceSize])

	out, ok := secretbox.Open(nil, raw[nonceSize:], &nonce, &e.key)
	if !ok {
		return "", op.Error(encryption.ErrAuthenticationFailed, "decrypting ciphered content")
	}

	return string(out), nil
}
