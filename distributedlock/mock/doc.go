// Package mock provides moq-generated mock implementations of the
// distributedlock package's interfaces.
package mock

// Regenerate the moq mocks via `go generate ./distributedlock/mock/`.

//go:generate go tool github.com/matryer/moq -out locker_mock.go -pkg mock -rm -fmt goimports .. Locker:LockerMock Lock:LockMock ScopedLocker:ScopedLockerMock
