// Package capitalismmock provides mock implementations of the capitalism package's
// interfaces. Both the hand-written testify-based MockPaymentManager and the
// moq-generated PaymentManagerMock live here during the testify → moq migration.
// New test code should prefer PaymentManagerMock.
package capitalismmock

// Regenerate the moq mocks via `go generate ./capitalism/mock/`.

//go:generate go tool github.com/matryer/moq -out payment_manager_mock.go -pkg capitalismmock -rm -fmt goimports .. PaymentManager:PaymentManagerMock
