package salsa20

import (
	"context"
	"encoding/base64"

	"golang.org/x/crypto/salsa20"
)

func (e *salsa20Impl) Encrypt(ctx context.Context, content string) (string, error) {
	_, span := e.tracer.StartSpan(ctx)
	defer span.End()

	out := make([]byte, len([]byte(content)))
	salsa20.XORKeyStream(out, []byte(content), []byte{0, 0, 0, 0, 0, 0, 0, 0}, &e.key)

	return base64.URLEncoding.EncodeToString(out), nil
}
