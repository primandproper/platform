// Package mock provides moq-generated mock implementations of interfaces in
// the authorization package. The primary consumer is external tests that need
// to mock authorization.PolicyResolver — authorization's own tests do not
// depend on this package.
//
// Note that there is nothing here for the request-time types. Grants is a
// struct, so a test builds the authority it wants directly with NewGrants,
// AllowAll, or DenyAll; and GrantsExtractor is a function, so a fake is a
// closure. Only policy resolution — the part that may do I/O — has an interface
// worth mocking.
package mock

// Regenerate the moq mocks via `go generate ./authorization/mock/`.

//go:generate go tool github.com/matryer/moq -out authorization_mock.go -pkg mock -rm -fmt goimports .. PolicyResolver:PolicyResolverMock
