// Package partitionedmock provides a moq-generated mock implementation of the partitioned
// package's KeyedCircuitBreaker interface.
package partitionedmock

// Regenerate the moq mocks via `go generate ./circuitbreaking/partitioned/mock/`.

//go:generate go tool github.com/matryer/moq -out keyedcircuitbreaker_mock.go -pkg partitionedmock -rm -fmt goimports .. KeyedCircuitBreaker:KeyedCircuitBreakerMock
