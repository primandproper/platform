// Package notificationsmock provides moq-generated mock implementations of
// interfaces in the notifications package. The primary consumers are external
// tests that need to stand in for notifications.Inbox or notifications.Registry
// without a database.
package notificationsmock

// Regenerate via `go generate ./notifications/mock/`.

//go:generate go tool github.com/matryer/moq -out notifications_mock.go -pkg notificationsmock -rm -fmt goimports .. Inbox:InboxMock Registry:RegistryMock
