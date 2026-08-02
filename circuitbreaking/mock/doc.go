// Package circuitbreakingmock provides moq-generated mock implementations of the
// circuitbreaking package's interfaces.
package circuitbreakingmock

// Regenerate the moq mocks via `go generate ./circuitbreaking/mock/`.

//go:generate go tool github.com/matryer/moq -out circuitbreaker_mock.go -pkg circuitbreakingmock -rm -fmt goimports .. CircuitBreaker:CircuitBreakerMock
