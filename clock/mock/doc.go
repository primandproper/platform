// Package clockmock provides moq-generated mock implementations of the clock
// package's interfaces. For most tests the manually-advanced clock in
// clock/fake is the better double; reach for these mocks when a test needs
// to assert on the exact calls a unit makes.
package clockmock

// Regenerate the moq mocks via `go generate ./clock/mock/`.

//go:generate go tool github.com/matryer/moq -out clock_mock.go -pkg clockmock -rm -fmt goimports .. Clock:ClockMock Ticker:TickerMock
