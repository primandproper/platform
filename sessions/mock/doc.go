// Package sessionsmock provides moq-generated mock implementations of
// interfaces in the sessions package. The primary consumer is external tests
// that need to mock sessions.Store[T] or sessions.Backend[T] — sessions' own
// tests do not depend on this package.
//
// BackendMock is the more useful of the two: a Store built over it exercises
// the real expiry policy, identifier minting, and observability, with only the
// storage faked.
package sessionsmock

// Regenerate via `go generate ./sessions/mock/`.

//go:generate go tool github.com/matryer/moq -out sessions_mock.go -pkg sessionsmock -rm -fmt goimports .. Store:StoreMock Backend:BackendMock
