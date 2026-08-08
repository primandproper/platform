package noop

import (
	"context"
	"time"

	"github.com/primandproper/platform-go/v10/embeddings"
)

var _ embeddings.Embedder = (*Embedder)(nil)

// Embedder is a no-op Embedder.
type Embedder struct{}

// NewEmbedder returns a no-op Embedder.
func NewEmbedder() embeddings.Embedder {
	return &Embedder{}
}

// GenerateEmbedding is a no-op that returns an empty vector.
func (*Embedder) GenerateEmbedding(_ context.Context, input *embeddings.Input) (*embeddings.Embedding, error) {
	if input == nil {
		return nil, embeddings.ErrNilInput
	}

	return &embeddings.Embedding{
		Vector:      []float32{},
		SourceText:  input.Content,
		Model:       "noop",
		Provider:    "noop",
		Dimensions:  0,
		GeneratedAt: time.Now(),
	}, nil
}

// GenerateEmbeddings returns one empty embedding per input.
func (e *Embedder) GenerateEmbeddings(ctx context.Context, inputs []*embeddings.Input) ([]*embeddings.Embedding, error) {
	out := make([]*embeddings.Embedding, len(inputs))
	for i, input := range inputs {
		emb, err := e.GenerateEmbedding(ctx, input)
		if err != nil {
			return nil, err
		}

		out[i] = emb
	}

	return out, nil
}
