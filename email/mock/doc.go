// Package emailmock provides mock implementations of the email package's
// interfaces. Both the hand-written testify-based Emailer type and the
// moq-generated EmailerMock type live here during the testify → moq
// migration. New test code should prefer the moq-generated types.
package emailmock

// Regenerate the moq mocks via `go generate ./email/mock/`.

//go:generate go tool github.com/matryer/moq -out emailer_mock.go -pkg emailmock -rm -fmt goimports .. Emailer:EmailerMock
