// Package identitymock provides moq-generated mock implementations of interfaces
// in the identity package. The primary consumers are external tests that need to
// stand in for identity.Store without a database.
package identitymock

// Regenerate via `go generate ./identity/mock/`.

//go:generate go tool github.com/matryer/moq -out identity_mock.go -pkg identitymock -rm -fmt goimports .. Store:StoreMock
