// Package randommock provides mock implementations of the random package's
// interfaces. Both the hand-written testify-based Generator and the
// moq-generated GeneratorMock live here during the testify → moq migration.
// New test code should prefer GeneratorMock.
package randommock

// Regenerate the moq mocks via `go generate ./random/mock/`.

//go:generate go tool github.com/matryer/moq -out generator_mock.go -pkg randommock -rm -fmt goimports .. Generator:GeneratorMock
