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

// NewNoopProvider returns a no-op provider that returns empty responses.
func NewNoopProvider() Provider {
	return &noopProvider{}
}

// noopProvider is a no-op implementation of Provider.
type noopProvider struct{}

// Completion implements Provider.
func (*noopProvider) Completion(_ context.Context, _ CompletionParams) (*CompletionResult, error) {
	return &CompletionResult{Content: ""}, nil
}
