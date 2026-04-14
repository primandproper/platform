// Package mock provides moq-generated mock implementations of interfaces in
// the totp package.
package mock

// Regenerate via `go generate ./authentication/totp/mock/`.

//go:generate go tool github.com/matryer/moq -out totp_mock.go -pkg mock -rm -fmt goimports .. Verifier:VerifierMock
