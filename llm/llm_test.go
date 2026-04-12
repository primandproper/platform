package llm

import (
	"testing"

	"github.com/shoenig/test"
)

func TestNoopProvider_Completion(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		provider := NewNoopProvider()

		result, err := provider.Completion(ctx, CompletionParams{
			Model: "test",
			Messages: []Message{
				{Role: "user", Content: "hello"},
			},
		})

		test.NoError(t, err)
		test.NotNil(t, result)
	})
}
