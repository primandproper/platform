package encryption

import (
	"context"
)

type (
	// KeyID names one key within a Keyring. It travels in the clear at the
	// front of every ciphertext, so it identifies a key without revealing
	// anything about it: use a short opaque label like "k1" or a date stamp,
	// never key material and never something the key material can be derived
	// from.
	//
	// A key ID is permanent. It is how a ciphertext written years ago says
	// which key opens it, so an ID that gets reused for different material
	// does not rotate a keyring, it corrupts one.
	KeyID string

	// MasterKey is secret key material used to encrypt and decrypt. It is a
	// named type over []byte so that dependency-injection lookups resolve it
	// distinctly and cannot collide with an arbitrary []byte value registered
	// in the same container.
	MasterKey []byte

	// Keyset is every key a Keyring should hold, by ID. It is the shape key
	// material arrives in from configuration and from dependency injection,
	// and it is a named type for the same reason MasterKey is.
	//
	// A Keyset is not itself a ring: it carries no notion of which key is
	// current, because that belongs with the configuration that names it
	// rather than with the material.
	Keyset map[KeyID]MasterKey

	// Encryptor encrypts plaintext under the current key.
	//
	// associatedData is authenticated but not encrypted: it is not recoverable
	// from the ciphertext, and decryption fails unless the same value is
	// supplied again. Passing the identity of the thing being encrypted — a
	// row's primary key, a tenant ID, a column name — is what stops a
	// ciphertext from being lifted out of one row and pasted into another,
	// which is otherwise undetectable. nil means no binding.
	Encryptor interface {
		Encrypt(ctx context.Context, plaintext, associatedData []byte) ([]byte, error)
	}

	// Decryptor decrypts ciphertext under whichever key produced it, provided
	// that key is still in the ring.
	//
	// associatedData has to match what Encrypt was given byte for byte.
	// A mismatch is reported as ErrAuthenticationFailed and is
	// indistinguishable from tampering, because it is not distinguishable from
	// tampering.
	Decryptor interface {
		Decrypt(ctx context.Context, ciphertext, associatedData []byte) ([]byte, error)
	}

	EncryptorDecryptor interface {
		Encryptor
		Decryptor
	}

	// Cipher is authenticated encryption under exactly one key, and it is the
	// seam a provider implements. It deliberately knows nothing about key IDs,
	// rotation, or framing — a Keyring composes Ciphers into a surface that
	// has all three, so a provider only has to get the cryptography right.
	//
	// Implementations must be AEADs. A Cipher that encrypts without
	// authenticating cannot honor associatedData and cannot report
	// ErrAuthenticationFailed, which are the two guarantees everything above
	// this interface is built on.
	Cipher interface {
		// Seal encrypts plaintext, authenticating both it and associatedData.
		Seal(ctx context.Context, plaintext, associatedData []byte) ([]byte, error)
		// Open reverses Seal, and returns ErrAuthenticationFailed if the
		// ciphertext or the associated data has changed.
		Open(ctx context.Context, ciphertext, associatedData []byte) ([]byte, error)
	}

	// KeyWrapper encrypts and decrypts key material against a key it does not
	// hand out. It is the seam for envelope encryption: a cloud KMS performs
	// wrap and unwrap inside its own boundary, so the key doing the wrapping
	// never enters this process and cannot leave in a heap dump.
	//
	// This is why it is a separate interface from Cipher rather than a use of
	// one. A Cipher is handed key material at construction; the entire value
	// of a KeyWrapper is that nothing ever hands you the key.
	//
	// Implementations that do hold the wrapping key locally are legitimate —
	// there is nothing better available behind an environment variable — but
	// they are a weaker thing wearing the same interface, and they should say
	// so in their own documentation.
	KeyWrapper interface {
		// Wrap encrypts key material. associatedData binds the result to a
		// context the same way it does for a Cipher.
		Wrap(ctx context.Context, key, associatedData []byte) ([]byte, error)
		// Unwrap reverses Wrap.
		Unwrap(ctx context.Context, wrapped, associatedData []byte) ([]byte, error)
	}
)
