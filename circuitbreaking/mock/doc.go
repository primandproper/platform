// Package mock provides mock implementations of the circuitbreaking package's
// interfaces. Both the hand-written testify-based MockCircuitBreaker and the
// moq-generated CircuitBreakerMock live here during the testify → moq
// migration. New test code should prefer CircuitBreakerMock.
package mock

// Regenerate the moq mocks via `go generate ./circuitbreaking/mock/`.

//go:generate go tool github.com/matryer/moq -out circuitbreaker_mock.go -pkg mock -rm -fmt goimports .. CircuitBreaker:CircuitBreakerMock
