package embeddings_test

import (
	"testing"

	"github.com/primandproper/platform-go/v9/embeddings"
	embeddingsnoop "github.com/primandproper/platform-go/v9/embeddings/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNoopEmbedder_GenerateEmbedding(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		embedder := embeddingsnoop.NewEmbedder()

		result, err := embedder.GenerateEmbedding(ctx, &embeddings.Input{
			Content: "hello world",
		})

		must.NoError(t, err)
		must.NotNil(t, result)
		test.EqOp(t, "hello world", result.SourceText)
		test.EqOp(t, "noop", result.Model)
		test.EqOp(t, "noop", result.Provider)
		test.EqOp(t, 0, result.Dimensions)
		test.SliceEmpty(t, result.Vector)
		test.False(t, result.GeneratedAt.IsZero())
	})
}

func TestEmbedder_batchContract(T *testing.T) {
	T.Parallel()

	// The contract every provider owes GenerateEmbeddings, asserted against the
	// noop embedder so it stays true of the shape rather than of one backend.
	e := embeddingsnoop.NewEmbedder()

	T.Run("returns one embedding per input, in order", func(t *testing.T) {
		t.Parallel()

		out, err := e.GenerateEmbeddings(t.Context(), []*embeddings.Input{
			{Content: "first"},
			{Content: "second"},
			{Content: "third"},
		})
		must.NoError(t, err)
		must.SliceLen(t, 3, out)

		test.EqOp(t, "first", out[0].SourceText)
		test.EqOp(t, "second", out[1].SourceText)
		test.EqOp(t, "third", out[2].SourceText)
	})

	T.Run("an empty batch is not an error", func(t *testing.T) {
		t.Parallel()

		out, err := e.GenerateEmbeddings(t.Context(), nil)
		must.NoError(t, err)
		test.SliceEmpty(t, out)
	})

	// The whole call fails rather than returning a slice with a hole in it.
	T.Run("a nil input fails the whole batch", func(t *testing.T) {
		t.Parallel()

		out, err := e.GenerateEmbeddings(t.Context(), []*embeddings.Input{{Content: "ok"}, nil})
		test.ErrorIs(t, err, embeddings.ErrNilInput)
		test.Nil(t, out)
	})
}
