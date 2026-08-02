// Package panickingmock provides moq-generated mock implementations of the
// panicking package's interfaces.
package panickingmock

// Regenerate the moq mocks via `go generate ./panicking/mock/`.

//go:generate go tool github.com/matryer/moq -out panicker_mock.go -pkg panickingmock -rm -fmt goimports .. Panicker:PanickerMock
