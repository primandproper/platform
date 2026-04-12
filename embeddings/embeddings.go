package embeddings

import (
	"context"
	"time"
)

// Input is the content to be embedded.
type Input struct {
	// Content is the text to embed.
	Content string

	// Model optionally overrides the provider's configured DefaultModel.
	// Leave empty to use the default from the provider's Config.
	Model string
}

// Embedding is the result of embedding a single piece of content.
// It carries provenance alongside the vector so that re-embedding
// and ETL pipelines can be driven from the stored result alone.
type Embedding struct {
	GeneratedAt time.Time
	SourceText  string
	Model       string
	Provider    string
	Vector      []float32
	Dimensions  int
}

// Embedder generates vector embeddings for text.
type Embedder interface {
	GenerateEmbedding(ctx context.Context, input *Input) (*Embedding, error)
}

type noopEmbedder struct{}

// NewNoopEmbedder returns an Embedder that returns an empty vector and no error.
// Intended for tests and local development.
func NewNoopEmbedder() Embedder {
	return &noopEmbedder{}
}

func (n *noopEmbedder) GenerateEmbedding(_ context.Context, input *Input) (*Embedding, error) {
	return &Embedding{
		Vector:      []float32{},
		SourceText:  input.Content,
		Model:       "noop",
		Provider:    "noop",
		Dimensions:  0,
		GeneratedAt: time.Now(),
	}, nil
}
