// Package embeddingsmock provides moq-generated mock implementations of the embeddings
// package's interfaces.
package embeddingsmock

// Regenerate the moq mocks via `go generate ./embeddings/mock/`.

//go:generate go tool github.com/matryer/moq -out embedder_mock.go -pkg embeddingsmock -rm -fmt goimports .. Embedder:EmbedderMock
