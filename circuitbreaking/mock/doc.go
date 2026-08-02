// Package mock provides moq-generated mock implementations of the
// circuitbreaking package's interfaces.
package mock

// Regenerate the moq mocks via `go generate ./circuitbreaking/mock/`.

//go:generate go tool github.com/matryer/moq -out circuitbreaker_mock.go -pkg mock -rm -fmt goimports .. CircuitBreaker:CircuitBreakerMock
