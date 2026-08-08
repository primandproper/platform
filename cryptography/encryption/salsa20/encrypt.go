package salsa20

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"

	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/keys"

	"golang.org/x/crypto/nacl/secretbox"
)

func (e *salsa20Impl) Encrypt(ctx context.Context, content string) (string, error) {
	_, op := e.o11y.Begin(ctx, observability.WithValue(keys.LengthKey, len(content)))
	defer op.End()

	var nonce [nonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return "", op.Error(err, "generating nonce")
	}

	sealed := secretbox.Seal(nonce[:], []byte(content), &nonce, &e.key)

	return base64.URLEncoding.EncodeToString(sealed), nil
}
