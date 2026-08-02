// Package randommock provides moq-generated mock implementations of the random
// package's interfaces.
package randommock

// Regenerate the moq mocks via `go generate ./random/mock/`.

//go:generate go tool github.com/matryer/moq -out generator_mock.go -pkg randommock -rm -fmt goimports .. Generator:GeneratorMock
