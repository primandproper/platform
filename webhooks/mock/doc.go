// Package webhooksmock provides moq-generated mock implementations of interfaces in
// the webhooks package. The primary consumers are external tests that need to
// stand in for webhooks.Store or webhooks.Dispatcher without a database.
package webhooksmock

// Regenerate via `go generate ./webhooks/mock/`.

//go:generate go tool github.com/matryer/moq -out webhooks_mock.go -pkg webhooksmock -rm -fmt goimports .. Store:StoreMock Dispatcher:DispatcherMock
