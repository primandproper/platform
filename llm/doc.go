/*
Package llm is the platform's interface to language models: content blocks,
tool calling, streaming, structured output, and token accounting, over
Anthropic and OpenAI.

The interface is the product here. Both backing providers are reached through
the same [Provider], and the differences between them — where the system prompt
goes, how a tool result is addressed, whether streamed tool arguments arrive
whole or in fragments, what a 429 looks like — are this package's problem
rather than the caller's.

# Content blocks

A [Message]'s content is a list of [Part]s rather than a string. A turn in which
the model says something, calls a tool, and then says something else is three
parts in order, and flattening it to text with the tool call hoisted into a
sidecar field loses the order. The common cases stay one-liners:

	req := &llm.CompletionRequest{
		Model:  "claude-sonnet-5",
		System: "You are terse.",
		Messages: []llm.Message{
			llm.UserText("What is the capital of France?"),
		},
	}

	resp, err := provider.Completion(ctx, req)
	if err != nil {
		return err
	}

	fmt.Println(resp.Text())

There is no system role. [CompletionRequest.System] is a field because Anthropic
takes the system prompt out-of-band and OpenAI takes it as a leading message,
and hiding that difference is the point.

# Tool calling

Declare tools on the request, and the model answers with [StopReasonToolUse] and
one or more [ToolUse] parts. Run them, and send the answers back as a single
tool message:

	resp, err := provider.Completion(ctx, req)
	if err != nil {
		return err
	}

	uses := resp.ToolUses()
	if len(uses) == 0 {
		return nil // the model is done
	}

	results := make([]llm.ToolResult, 0, len(uses))
	for _, use := range uses {
		output, err := run(ctx, use)
		results = append(results, llm.ToolResult{
			ToolUseID: use.ID,
			Content:   output,
			IsError:   err != nil,
		})
	}

	req.Messages = append(req.Messages,
		llm.Message{Role: llm.RoleAssistant, Content: resp.Content},
		llm.ToolResultMessage(results...),
	)

Appending the assistant's whole Content, rather than its text, is what keeps the
tool call in the transcript — without it the next turn sees results for calls it
has no record of making.

[ToolUse.Input] is model output and therefore untrusted. It may not validate
against the tool's schema, and a response that stopped at [StopReasonMaxTokens]
may leave it invalid JSON outright.

# Streaming

[Provider.Stream] returns a [Stream], which must be closed. See [Stream] for the
loop shape and for why it is an explicit iterator rather than an iter.Seq2.

Streamed tool calls are the one place where the underlying providers disagree
outright, and where this package earns its keep. OpenAI streams tool arguments
as raw fragments, with the call's ID and name present only on the first;
Anthropic accumulates internally and re-emits the whole call each time. A
consumer written against either one is wrong on the other. This package emits
[EventToolUse] only once a call is complete, so the same loop works on both.

# Errors

Every provider error matches one of this package's sentinels under errors.Is —
[ErrRateLimited], [ErrContextTooLong], [ErrAuthentication], [ErrModelNotFound],
[ErrContentFiltered], [ErrInvalidRequest], [ErrUnsupportedFeature] — or none of
them, when the failure has no platform-level meaning. Callers never need the
client library underneath:

	resp, err := provider.Completion(ctx, req)

	var rateLimited *llm.RateLimitError
	switch {
	case errors.As(err, &rateLimited):
		time.Sleep(rateLimited.RetryAfter)
	case errors.Is(err, llm.ErrContextTooLong):
		return errors.Wrap(err, "prompt is too long to retry")
	case err != nil:
		return err
	}

No method here retries. Rate limits are reported, not absorbed, because the
right backoff belongs to the caller's own budget; wrap the call with the
platform's retry package to get one.

# Capabilities

[Provider.Capabilities] reports what a provider supports, so a caller can
degrade deliberately instead of finding out mid-conversation. It describes the
provider and not the model: a provider that supports images still has models
that do not.

# Choosing a provider

The llm/config subpackage builds a Provider from configuration, and the provider
name is required: an unset or unrecognized one wraps errors.ErrUnknownProvider
rather than standing something up. The llm/noop provider is selectable, by naming
it, and a service that wants to run without LLM credentials has to say so —
because a provider that quietly answers from a canned response looks like a
healthy deployment whose answers have become useless.

# Why the provider files look alike

The openai and anthropic implementations are near-identical files, and that is
deliberate. Each is a translation between this package's request and response
types and one vendor's SDK, so the shape they share is the shape of the
interface — request in, translate, call, translate back, instrument — and the
lines that differ are precisely the ones a reader came to see.

Factoring the common half out would produce a base type holding the
instrumentation and the error mapping, with each vendor supplying a handful of
translation hooks. That trades a file a reader can hold in their head for two
files they must hold at once, and it does it at the seam most likely to move:
every vendor difference that shows up next — a new content block type, a
streaming protocol that frames differently, a usage field one of them omits —
arrives as a hook the base type did not anticipate. The generic machinery
around three implementations is not obviously cheaper than three
implementations.

What was extracted is what is genuinely one decision rather than one shape:
the instrument trio (observability/metrics.OperationSet), the latency timing
(observability.Operation.Time), and the float narrowing every embeddings
provider needs (embeddings.ToFloat32). The rule those share is that a second
copy could be *wrong* — a different suffix, a bypassed clock, a different
precision. A second copy of "decode the response, build a Completion" cannot
be wrong in that way; it can only be different, and different is what it is
for.
*/
package llm
