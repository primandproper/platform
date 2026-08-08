package noop

import (
	"context"

	"github.com/primandproper/platform-go/v10/llm"
)

var _ llm.Provider = (*Provider)(nil)

// name is what Name reports. It is a real provider name rather than the empty
// string so that a metric or a log line broken down by provider says "noop"
// instead of losing the dimension.
const name = "noop"

// Provider is a no-op Provider. It is what llm/config hands back when no
// provider is configured, so that a service without LLM credentials starts and
// runs — answering nothing — rather than failing at construction.
type Provider struct{}

// NewProvider returns a no-op Provider.
func NewProvider() llm.Provider {
	return &Provider{}
}

// Name implements llm.Provider.
func (*Provider) Name() string {
	return name
}

// Capabilities implements llm.Provider. Everything is false: Stream returns an
// empty stream rather than a streaming one, and no request feature is honored
// because no request is sent.
func (*Provider) Capabilities() llm.Capabilities {
	return llm.Capabilities{}
}

// Completion is a no-op that returns an empty response.
func (*Provider) Completion(context.Context, *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{StopReason: llm.StopReasonEndTurn}, nil
}

// Stream is a no-op that returns a stream yielding only llm.EventDone, so a
// consumer's event loop runs to completion instead of special-casing this
// provider.
func (*Provider) Stream(context.Context, *llm.CompletionRequest) (llm.Stream, error) {
	return llm.NewSliceStream(llm.Event{Type: llm.EventDone, StopReason: llm.StopReasonEndTurn}), nil
}
