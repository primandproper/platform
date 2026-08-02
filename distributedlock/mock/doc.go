// Package distributedlockmock provides moq-generated mock implementations of the
// distributedlock package's interfaces.
package distributedlockmock

// Regenerate the moq mocks via `go generate ./distributedlock/mock/`.

//go:generate go tool github.com/matryer/moq -out locker_mock.go -pkg distributedlockmock -rm -fmt goimports .. Locker:LockerMock Lock:LockMock ScopedLocker:ScopedLockerMock
