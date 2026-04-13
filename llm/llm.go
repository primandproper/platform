package llm

import (
	"context"
)

// Message represents a chat message.
type Message struct {
	Role    string // "user", "assistant", "system", "tool"
	Content string
}

// CompletionParams represents parameters for a completion request.
type CompletionParams struct {
	Model    string
	Messages []Message
}

// CompletionResult represents the result of a completion request.
type CompletionResult struct {
	Content string
}

// Provider is the interface for LLM providers.
type Provider interface {
	Completion(ctx context.Context, params CompletionParams) (*CompletionResult, error)
}
