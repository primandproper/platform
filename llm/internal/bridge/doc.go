/*
Package bridge translates between the platform's llm types and any-llm-go's.

It exists because the translation is identical for Anthropic and OpenAI, and
writing it twice would be two chances to get the streaming accumulator wrong.
any-llm-go normalizes both providers onto OpenAI's wire shape — a flat message
with sidecar tool calls — and the platform's model is Anthropic-shaped ordered
content blocks, so something has to sit between them. That something is here,
and the provider packages shrink to construction, observability, and capability
reporting.

Three jobs:

Params and Response translate a request and its answer. The lossy edges are
documented on the functions that lose something.

NormalizeError maps any-llm-go's sentinels and typed errors onto the platform's,
so that a caller never has to import any-llm-go to find out it was rate limited.

Stream adapts any-llm-go's chunk/error channel pair to llm.Stream, and is where
the accumulator lives. any-llm-go does not normalize streaming tool calls, and
the two providers disagree: OpenAI passes deltas through raw, so arguments
arrive as JSON fragments and the ID and name appear only on the first; Anthropic
accumulates internally and re-emits the cumulative call every time. Stream
accumulates either into one complete llm.EventToolUse. See accumulateToolCall
for how it tells the two apart.
*/
package bridge
