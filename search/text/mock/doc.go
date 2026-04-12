/*
Package mocksearch provides moq-generated mocks for the search/text package.
*/
package mocksearch

// Regenerate the moq mocks via `go generate ./search/text/mock/`.

//go:generate go tool github.com/matryer/moq -out index_mock.go -pkg mocksearch -rm -fmt goimports .. Index:IndexMock
