package encryption

import (
	"context"
)

type (
	Encryptor interface {
		Encrypt(ctx context.Context, content string) (string, error)
	}

	Decryptor interface {
		Decrypt(ctx context.Context, content string) (string, error)
	}

	EncryptorDecryptor interface {
		Encryptor
		Decryptor
	}
)
