package llm_test

import (
	"context"
	"fmt"

	"github.com/primandproper/platform/llm"
)

func ExampleNewNoopProvider() {
	provider := llm.NewNoopProvider()

	result, err := provider.Completion(context.Background(), llm.CompletionParams{
		Model: "example-model",
		Messages: []llm.Message{
			{Role: "user", Content: "Hello!"},
		},
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("content: %q\n", result.Content)
	// Output: content: ""
}
