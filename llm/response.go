package llm

import (
	"strings"
)

// StopReason is why the model stopped generating.
type StopReason string

// The reasons a model stops.
const (
	// StopReasonEndTurn means the model finished what it had to say.
	StopReasonEndTurn StopReason = "end_turn"
	// StopReasonMaxTokens means the response hit CompletionRequest.MaxTokens
	// and is truncated. Any tool call in a truncated response may carry
	// incomplete JSON.
	StopReasonMaxTokens StopReason = "max_tokens"
	// StopReasonStopSequence means one of CompletionRequest.StopSequences was
	// produced.
	StopReasonStopSequence StopReason = "stop_sequence"
	// StopReasonToolUse means the model is waiting on tool results. The
	// response's ToolUses are the calls to run.
	StopReasonToolUse StopReason = "tool_use"
	// StopReasonContentFilter means the provider's safety filter stopped the
	// response.
	StopReasonContentFilter StopReason = "content_filter"
)

// CompletionResponse is one completion.
type CompletionResponse struct {
	// Usage is the token accounting for the request, when the provider
	// reported it.
	Usage *Usage
	// ID is the provider's identifier for the response.
	ID string
	// Model is the model that actually answered, which can differ from the one
	// requested when the provider resolves an alias.
	Model string
	// StopReason is why generation ended.
	StopReason StopReason
	// Content is what the model produced, in order. See Part.
	Content []Part
}

// Text concatenates the response's text parts, ignoring reasoning and tool
// calls. A nil receiver returns the empty string, so a caller can chain it off
// a call it has not error-checked.
func (r *CompletionResponse) Text() string {
	if r == nil {
		return ""
	}

	var sb strings.Builder
	for i := range r.Content {
		if r.Content[i].Type == PartText {
			sb.WriteString(r.Content[i].Text)
		}
	}

	return sb.String()
}

// ToolUses returns the tool calls the model requested, in order. It returns
// nil when there are none, which is the signal that a tool-calling loop has
// reached its end.
func (r *CompletionResponse) ToolUses() []ToolUse {
	if r == nil {
		return nil
	}

	var uses []ToolUse
	for i := range r.Content {
		if r.Content[i].Type == PartToolUse && r.Content[i].ToolUse != nil {
			uses = append(uses, *r.Content[i].ToolUse)
		}
	}

	return uses
}

// Usage is the token accounting for one request.
type Usage struct {
	// InputTokens is what the prompt cost.
	InputTokens int
	// OutputTokens is what the response cost, including reasoning tokens where
	// the provider bills them that way.
	OutputTokens int
	// ReasoningTokens is the reasoning portion, when the provider breaks it
	// out.
	ReasoningTokens int
	// TotalTokens is the provider's own total. It is reported rather than
	// derived, because a provider that discounts or surcharges some tokens
	// disagrees with InputTokens + OutputTokens on purpose.
	TotalTokens int
}
