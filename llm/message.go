package llm

import (
	"encoding/json"
	"strings"
)

// Role identifies who produced a Message.
//
// There is deliberately no system role. A system prompt is
// CompletionRequest.System, because the providers disagree about what it is:
// Anthropic takes it out-of-band as a top-level request parameter, OpenAI takes
// it as a leading message in the conversation. Modeling it as a role would
// make callers responsible for a difference this package exists to hide.
type Role string

// The roles a Message can carry.
const (
	// RoleUser marks input from the caller's end user.
	RoleUser Role = "user"
	// RoleAssistant marks output the model produced, replayed back to it on a
	// later turn.
	RoleAssistant Role = "assistant"
	// RoleTool marks the results of tool calls the model asked for. A tool
	// message carries only PartToolResult parts.
	RoleTool Role = "tool"
)

// PartType discriminates the union in Part.
type PartType string

// The kinds of content a Part can hold.
const (
	// PartText is a run of plain text, in Part.Text.
	PartText PartType = "text"
	// PartImage is an image, in Part.Image.
	PartImage PartType = "image"
	// PartToolUse is the model's request to call a tool, in Part.ToolUse.
	PartToolUse PartType = "tool_use"
	// PartToolResult is the outcome of such a call, in Part.ToolResult.
	PartToolResult PartType = "tool_result"
	// PartThinking is the model's reasoning, in Part.Text.
	PartThinking PartType = "thinking"
)

// Part is one piece of a message's content. Type says which of the remaining
// fields is meaningful; the others are nil or zero.
//
// Content is a list of parts rather than a string with sidecar fields because
// order matters. A model that emits text, then a tool call, then more text has
// said three things in sequence, and replaying that turn with the tool call
// hoisted into a separate field loses the sequence.
type Part struct {
	Image      *Image
	ToolUse    *ToolUse
	ToolResult *ToolResult
	Type       PartType
	Text       string
}

// Image is an image attached to a message. Exactly one of Data or URL carries
// the image; when both are set, Data wins.
type Image struct {
	// URL is an http(s) URL or a data: URI the provider fetches or decodes.
	URL string
	// MediaType is the IANA media type of Data, e.g. "image/png". It is
	// required with Data and ignored with URL, whose own media type is either
	// implied by the server or already inside the data URI.
	MediaType string
	// Data is the raw image bytes, which the provider layer encodes as a data
	// URI. MediaType must be set alongside it.
	Data []byte
}

// ToolUse is the model's request to call one tool.
type ToolUse struct {
	// ID correlates this call with the ToolResult that answers it.
	ID string
	// Name is the Tool.Name the model chose.
	Name string
	// Input is the tool's arguments as JSON, shaped by the Tool's Schema. It is
	// the model's output and therefore untrusted: it may not validate against
	// the schema, and it may not be valid JSON at all if the model was cut off.
	Input json.RawMessage
}

// ToolResult is the outcome of one tool call, sent back to the model.
type ToolResult struct {
	// ToolUseID is the ID of the ToolUse this answers.
	ToolUseID string
	// Content is what the tool produced, as text. Structured results should be
	// marshaled to JSON by the caller.
	Content string
	// IsError reports that the tool failed. The providers' normalized wire
	// shape has no flag for this, so it is conveyed to the model by prefixing
	// Content with "error: " — the model still learns the call failed, which is
	// the point, but the transport is a convention rather than a field.
	IsError bool
}

// Message is one turn in a conversation.
type Message struct {
	Role    Role
	Content []Part
}

// UserText returns a user message holding a single run of text, which is the
// common case.
func UserText(text string) Message {
	return Message{
		Role:    RoleUser,
		Content: []Part{{Type: PartText, Text: text}},
	}
}

// AssistantText returns an assistant message holding a single run of text.
func AssistantText(text string) Message {
	return Message{
		Role:    RoleAssistant,
		Content: []Part{{Type: PartText, Text: text}},
	}
}

// ToolResultMessage returns the tool message answering one or more tool calls.
// Answering every call from a single assistant turn in one message is the
// shape both providers expect.
func ToolResultMessage(results ...ToolResult) Message {
	content := make([]Part, 0, len(results))
	for i := range results {
		result := results[i]
		content = append(content, Part{Type: PartToolResult, ToolResult: &result})
	}

	return Message{Role: RoleTool, Content: content}
}

// Text concatenates the message's text parts, ignoring images, tool calls, tool
// results, and reasoning. It is the "just give me the words" accessor; a caller
// that cares about the other parts should range over Content.
func (m Message) Text() string {
	var sb strings.Builder
	for i := range m.Content {
		if m.Content[i].Type == PartText {
			sb.WriteString(m.Content[i].Text)
		}
	}

	return sb.String()
}
