// Package mock provides moq-generated mock implementations of interfaces in
// the tokens package. The primary consumer is external tests that need to
// mock tokens.Issuer or tokens.Claims — tokens' own tests do not depend on
// this package.
package mock

// Regenerate via `go generate ./authentication/tokens/mock/`.

//go:generate go tool github.com/matryer/moq -out tokens_mock.go -pkg mock -rm -fmt goimports .. Issuer:IssuerMock Claims:ClaimsMock
