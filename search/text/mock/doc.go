/*
Package textsearchmock provides moq-generated mocks for the search/text package.
*/
package textsearchmock

// Regenerate the moq mocks via `go generate ./search/text/mock/`.

//go:generate go tool github.com/matryer/moq -out index_mock.go -pkg textsearchmock -rm -fmt goimports .. Index:IndexMock
