// Package noop is the random.Generator that generates nothing: every method
// returns an empty string or an empty byte slice, with a nil error.
//
// Read that against what the real generator is for. This interface backs
// two-factor secrets, salts, session and API tokens, and one-time codes, and an
// empty value with no error is indistinguishable at the call site from a
// successful draw. A system wired with this generator issues the same empty
// token to every caller, and comparing two of those tokens succeeds — so it is
// not merely unrandom, it is an authentication bypass that reports success at
// every step.
//
// It exists for tests that need a Generator to satisfy a signature and never
// read what it produced. There is no random/config, so nothing selects it from
// configuration; a caller who wants it names it in code, which is the only
// place a decision like this one should be visible.
package noop

import (
	"context"

	"github.com/primandproper/platform-go/v10/random"
)

var _ random.Generator = (*Generator)(nil)

// Generator is a no-op Generator.
type Generator struct{}

// NewGenerator returns a no-op Generator.
func NewGenerator() random.Generator {
	return &Generator{}
}

// GenerateHexEncodedString is a no-op.
func (*Generator) GenerateHexEncodedString(context.Context, int) (string, error) {
	return "", nil
}

// GenerateBase32EncodedString is a no-op.
func (*Generator) GenerateBase32EncodedString(context.Context, int) (string, error) {
	return "", nil
}

// GenerateBase64EncodedString is a no-op.
func (*Generator) GenerateBase64EncodedString(context.Context, int) (string, error) {
	return "", nil
}

// GenerateRawBytes is a no-op.
func (*Generator) GenerateRawBytes(context.Context, int) ([]byte, error) {
	return []byte{}, nil
}
