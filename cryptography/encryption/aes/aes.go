package aes

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"

	"github.com/primandproper/platform-go/v10/cryptography/encryption"
	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"
)

const name = "aes_cipher"

// KeyLength is the key size this package accepts, in bytes. AES-256 is the
// only size offered: a configurable one invites a 16-byte key chosen by
// accident, and nothing here is short of entropy.
const KeyLength = 32

// aesImpl is an AES-256-GCM Cipher.
type aesImpl struct {
	o11y observability.Observer
	aead cipher.AEAD
	// random is the nonce source, always crypto/rand.Reader outside this
	// package's own tests. It is a field rather than a direct call so the
	// exhausted-entropy path can be exercised without swapping a global that
	// every other parallel test in the process would see.
	random io.Reader
}

var _ encryption.Cipher = (*aesImpl)(nil)

// NewCipher builds an AES-256-GCM Cipher over key.
//
// The AEAD is constructed once here rather than per operation. Key schedule
// setup is not free, and doing it on every Encrypt was a per-row cost paid for
// nothing.
func NewCipher(key []byte, opts ...Option) (encryption.Cipher, error) {
	if len(key) != KeyLength {
		return nil, errors.Wrapf(encryption.ErrIncorrectKeyLength, "aes cipher: want %d bytes, got %d", KeyLength, len(key))
	}

	o := newOptions(opts)

	// Neither of the next two errors is reachable with a key this constructor
	// has already length-checked: crypto/aes rejects only wrong key sizes, and
	// cipher.NewGCM rejects only non-16-byte block sizes. They are forwarded
	// rather than ignored because the alternative is discarding an error the
	// standard library might one day return for a reason not listed here.
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.Wrap(err, "aes cipher: creating block cipher")
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.Wrap(err, "aes cipher: creating gcm")
	}

	return &aesImpl{
		o11y:   observability.NewObserver(name, o.logger, o.tracerProvider),
		aead:   aead,
		random: rand.Reader,
	}, nil
}
