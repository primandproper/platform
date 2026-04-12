// Package mock provides mock implementations of the distributedlock package's
// interfaces. Both the hand-written testify-based Locker/Lock types and the
// moq-generated LockerMock/LockMock types live here during the testify → moq
// migration. New test code should prefer the moq-generated types.
package mock

// Regenerate the moq mocks via `go generate ./distributedlock/mock/`.

//go:generate go tool github.com/matryer/moq -out locker_mock.go -pkg mock -rm -fmt goimports .. Locker:LockerMock Lock:LockMock
