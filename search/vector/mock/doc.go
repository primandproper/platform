// Package mock provides moq-generated mocks for the search/vector package.
package mock

// Regenerate the moq mocks via `go generate ./search/vector/mock/`.

//go:generate go tool github.com/matryer/moq -out index_mock.go -pkg mock -rm -fmt goimports .. Index:IndexMock
