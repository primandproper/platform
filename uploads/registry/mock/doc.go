// Package registrymock provides moq-generated mock implementations of
// interfaces in the uploads/registry package. The primary consumers are
// external tests that need to stand in for registry.Store without a database.
package registrymock

// Regenerate via `go generate ./uploads/registry/mock/`.

//go:generate go tool github.com/matryer/moq -out registry_mock.go -pkg registrymock -rm -fmt goimports .. Store:StoreMock
