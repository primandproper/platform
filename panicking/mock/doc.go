// Package mockpanicking provides moq-generated mock implementations of the
// panicking package's interfaces.
package mockpanicking

// Regenerate the moq mocks via `go generate ./panicking/mock/`.

//go:generate go tool github.com/matryer/moq -out panicker_mock.go -pkg mockpanicking -rm -fmt goimports .. Panicker:PanickerMock
