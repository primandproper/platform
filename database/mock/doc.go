/*
Package databasemock provides moq-generated mocks for the database package.
*/
package databasemock

// Regenerate the moq mocks via `go generate ./database/mock/`.

//go:generate go tool github.com/matryer/moq -out database_mock.go -pkg databasemock -rm -fmt goimports .. Client:ClientMock RawAccess:RawAccessMock ResultIterator:ResultIteratorMock SQLQueryExecutor:SQLQueryExecutorMock
