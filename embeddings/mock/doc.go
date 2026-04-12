// Package mock provides mock implementations of the embeddings package's
// interfaces. Both the hand-written testify-based Embedder and the moq-generated
// EmbedderMock live here during the testify → moq migration. New test code
// should prefer EmbedderMock.
package mock

// Regenerate the moq mocks via `go generate ./embeddings/mock/`.

//go:generate go tool github.com/matryer/moq -out embedder_mock.go -pkg mock -rm -fmt goimports .. Embedder:EmbedderMock
