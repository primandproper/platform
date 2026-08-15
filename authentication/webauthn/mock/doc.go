// Package webauthnmock provides moq-generated mock implementations of
// interfaces in the webauthn package.
package webauthnmock

// Regenerate via `go generate ./authentication/webauthn/mock/`.

//go:generate go tool github.com/matryer/moq -out webauthn_mock.go -pkg webauthnmock -rm -fmt goimports .. SessionStore:SessionStoreMock
