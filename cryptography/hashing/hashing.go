package hashing

import (
	"encoding/hex"
)

type (
	// Hasher reduces content to a fixed-size digest.
	//
	// Hash returns the raw digest bytes rather than an encoding of them, so
	// callers that need an integer, a prefix, or a non-hex encoding are not
	// forced through a string. Hex covers the common case.
	//
	// There is no error return: every implementation here wraps a hash.Hash,
	// whose Write is documented never to fail, so an error would only ever be
	// an unreachable branch at each call site.
	//
	// NOTE: implementations of this interface vary in cryptographic strength.
	// The sha256 and sha512 implementations are cryptographic hashes; the
	// adler32, crc64, and fnv implementations are NON-CRYPTOGRAPHIC checksums
	// and MUST NOT be selected for security, password, or tamper-resistance
	// purposes. Choose the implementation deliberately.
	//
	// A Hasher that only ever hashes one in-memory buffer belongs here; one
	// that needs streaming, Size, or BlockSize should take a hash.Hash
	// directly rather than widening this interface.
	Hasher interface {
		Hash(content []byte) []byte
	}
)

// Hex returns h's digest of content, hex-encoded. It is the conventional
// rendering of a digest for logs, storage, and comparison; reach past it to
// Hasher.Hash only when the raw bytes matter.
func Hex(h Hasher, content []byte) string {
	return hex.EncodeToString(h.Hash(content))
}

// HexString is Hex for a string input, saving callers that already hold a
// string a conversion at every call site.
func HexString(h Hasher, content string) string {
	return Hex(h, []byte(content))
}
