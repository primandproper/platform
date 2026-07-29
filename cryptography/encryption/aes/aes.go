package aes

import (
	"github.com/primandproper/platform-go/v8/cryptography/encryption"
	"github.com/primandproper/platform-go/v8/observability"
)

const name = "aes_encryptor"

// aesImpl is the standard EncryptorDecryptor implementation.
type aesImpl struct {
	o11y observability.Observer
	key  [32]byte
}

func NewEncryptorDecryptor(key []byte, opts ...Option) (encryption.EncryptorDecryptor, error) {
	if len(key) != 32 {
		return nil, encryption.ErrIncorrectKeyLength
	}

	o := newOptions(opts)

	var key32 [32]byte
	copy(key32[:], key)

	return &aesImpl{
		o11y: observability.NewObserver(name, o.logger, o.tracerProvider),
		key:  key32,
	}, nil
}
