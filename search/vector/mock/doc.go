// Package vectorsearchmock provides moq-generated mocks for the search/vector package.
package vectorsearchmock

// Regenerate the moq mocks via `go generate ./search/vector/mock/`.

//go:generate go tool github.com/matryer/moq -out index_mock.go -pkg vectorsearchmock -rm -fmt goimports .. Index:IndexMock
