package llm

import (
	"context"
)

// Capabilities is what a provider can do, reported ahead of time so that a
// caller can degrade deliberately instead of discovering the gap as an
// ErrUnsupportedFeature mid-conversation.
//
// It describes the provider, not the model. A provider that supports images
// still has models that do not, and only the request will find that out.
type Capabilities struct {
	// Streaming reports whether Provider.Stream produces real incremental
	// output. It is false for providers whose Stream is a formality.
	Streaming bool
	// Tools reports whether CompletionRequest.Tools is honored.
	Tools bool
	// Images reports whether PartImage is honored.
	Images bool
	// Reasoning reports whether CompletionRequest.ReasoningEffort is honored
	// and PartThinking parts come back.
	Reasoning bool
	// StructuredOutput reports whether CompletionRequest.ResponseFormat is
	// honored.
	StructuredOutput bool
}

// Provider is a language model that answers completions.
//
// Both methods take the same request, and the difference is only in delivery:
// Completion waits for the whole answer, Stream yields it as it arrives.
// Streaming is on the interface rather than behind an optional one because
// every real provider streams, and a caller that has to type-assert for it ends
// up writing the non-streaming path anyway.
type Provider interface {
	// Name identifies the provider, e.g. "anthropic". It is stable and safe to
	// use as a metric label or a persisted discriminator.
	Name() string
	// Capabilities reports what the provider supports.
	Capabilities() Capabilities
	// Completion answers the request in full.
	Completion(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
	// Stream answers the request incrementally. The returned Stream must be
	// closed. The error is for failures that happen before the stream starts;
	// failures during it surface from Stream.Err.
	Stream(ctx context.Context, req *CompletionRequest) (Stream, error)
}
