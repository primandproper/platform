// Package mock provides moq-generated mock implementations of interfaces in
// the webhooks package. The primary consumers are external tests that need to
// stand in for webhooks.Store or webhooks.Dispatcher without a database.
package mock

// Regenerate via `go generate ./webhooks/mock/`.

//go:generate go tool github.com/matryer/moq -out webhooks_mock.go -pkg mock -rm -fmt goimports .. Store:StoreMock Dispatcher:DispatcherMock
