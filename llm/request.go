package llm

// ReasoningEffort asks the model to spend more or less effort thinking before
// it answers. Providers implement it differently — Anthropic turns it into a
// thinking token budget, OpenAI passes it through to reasoning models — and
// providers or models that do not support it ignore it rather than failing.
type ReasoningEffort string

// The reasoning effort levels.
const (
	// ReasoningEffortNone disables reasoning. It is also the zero value's
	// meaning: an unset ReasoningEffort leaves the parameter off the request
	// entirely, which lets the provider apply its own default.
	ReasoningEffortNone ReasoningEffort = "none"
	// ReasoningEffortLow spends the least effort.
	ReasoningEffortLow ReasoningEffort = "low"
	// ReasoningEffortMedium is the middle setting.
	ReasoningEffortMedium ReasoningEffort = "medium"
	// ReasoningEffortHigh spends the most effort.
	ReasoningEffortHigh ReasoningEffort = "high"
	// ReasoningEffortAuto lets the provider decide.
	ReasoningEffortAuto ReasoningEffort = "auto"
)

// CompletionRequest is one request for a completion.
//
// The pointer-typed fields are the ones where "unset" and "zero" differ:
// Temperature 0 is a meaningful temperature, and so is a MaxTokens the caller
// never chose. A nil field is left off the wire so the provider's own default
// applies.
type CompletionRequest struct {
	// Temperature controls randomness, conventionally in [0, 2].
	Temperature *float64
	// TopP controls nucleus sampling, in [0, 1]. Setting it alongside
	// Temperature is accepted but rarely what anyone wants.
	TopP *float64
	// MaxTokens caps the response length. Anthropic requires one and the
	// provider layer supplies a default when this is nil.
	MaxTokens *int
	// Seed asks for reproducible sampling. Best-effort everywhere.
	Seed *int
	// ParallelToolCalls allows the model to request several tools at once.
	ParallelToolCalls *bool
	// ResponseFormat constrains the response to a JSON schema.
	ResponseFormat *ResponseFormat
	// ToolChoice constrains which of Tools the model may call.
	ToolChoice *ToolChoice
	// Model is the provider's model identifier. Empty means the provider's
	// configured default.
	Model string
	// System is the system prompt. See Role for why it is a field rather than a
	// message.
	System string
	// ReasoningEffort asks for extended thinking.
	ReasoningEffort ReasoningEffort
	// Messages is the conversation so far, oldest first. At least one is
	// required.
	Messages []Message
	// Tools are the tools the model may call.
	Tools []Tool
	// StopSequences are strings that end generation when produced.
	StopSequences []string
}

// Tool is one tool the model may call.
type Tool struct {
	// Schema is the JSON Schema describing the tool's input, as a decoded
	// object. It reaches the provider unchanged.
	Schema map[string]any
	// Name is what the model names in a ToolUse.
	Name string
	// Description tells the model when to call the tool. It is prompt text and
	// carries most of the weight of getting tool use right.
	Description string
}

// ToolChoiceMode is how strongly the model is steered toward calling a tool.
type ToolChoiceMode string

// The tool choice modes.
const (
	// ToolChoiceAuto lets the model decide whether to call a tool.
	ToolChoiceAuto ToolChoiceMode = "auto"
	// ToolChoiceRequired obliges the model to call some tool.
	ToolChoiceRequired ToolChoiceMode = "required"
	// ToolChoiceNone forbids tool calls for this turn.
	ToolChoiceNone ToolChoiceMode = "none"
	// ToolChoiceSpecific obliges the model to call the named tool.
	ToolChoiceSpecific ToolChoiceMode = "specific"
)

// ToolChoice constrains the model's choice of tool.
type ToolChoice struct {
	// Name is the tool to call. It is required when Mode is
	// ToolChoiceSpecific and ignored otherwise.
	Name string
	// Mode is how the choice is constrained. The zero value is not a valid
	// mode; leave ToolChoice nil rather than sending an empty one.
	Mode ToolChoiceMode
}

// ResponseFormat constrains the response to match a JSON schema, for callers
// that want to unmarshal the answer rather than read it.
type ResponseFormat struct {
	// Schema is the JSON Schema the response must satisfy, as a decoded object.
	Schema map[string]any
	// Name identifies the schema to the provider. Required.
	Name string
	// Strict asks the provider to guarantee conformance rather than merely
	// prompt for it, where it can.
	Strict bool
}
