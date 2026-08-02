// Package emailmock provides moq-generated mock implementations of the email
// package's interfaces.
package emailmock

// Regenerate the moq mocks via `go generate ./email/mock/`.

//go:generate go tool github.com/matryer/moq -out emailer_mock.go -pkg emailmock -rm -fmt goimports .. Emailer:EmailerMock
