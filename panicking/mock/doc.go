// Package mockpanicking provides mock implementations of the panicking package's
// interfaces. Both the hand-written testify-based Panicker and the moq-generated
// PanickerMock live here during the testify → moq migration. New test code
// should prefer PanickerMock.
package mockpanicking

// Regenerate the moq mocks via `go generate ./panicking/mock/`.

//go:generate go tool github.com/matryer/moq -out panicker_mock.go -pkg mockpanicking -rm -fmt goimports .. Panicker:PanickerMock
