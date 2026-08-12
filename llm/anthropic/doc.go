/*
Package anthropic is the Anthropic-backed llm.Provider.

Choosing it commits a deployment to an Anthropic API key, which is the only
required field. BaseURL and Timeout are passed to the client underneath;
everything else about the request — messages, tools, streaming, structured
output — is llm's vocabulary, translated here.

Capabilities reports streaming, tools, images, reasoning, and structured output,
the same set the openai sibling reports. The two providers are interchangeable
at the interface, and the reason to pick one is the models behind it rather than
anything the interface exposes.

# The fallback model floats on purpose

When neither the request nor Config names a model, requests go to
claude-sonnet-5. It is an alias rather than a dated snapshot because a snapshot
retires on a published schedule and then starts failing with no change on this
side — a production outage on a date nobody wrote down. Sonnet tier is the
default because this is what a caller who named no model at all gets, and a
library should not spend their money on the largest option by default.

# What is not done here

No retry. A rate limit arrives as an error matching llm.ErrRateLimited, usually
a *llm.RateLimitError carrying the provider's own advice about how long to wait;
choosing a backoff against that advice needs the caller's deadline, which this
package does not have. Wrap the call with the platform's retry package.

Stream's span and latency measurement cover the whole stream rather than the
call that opens it, so a consumer that abandons a stream without closing it
leaves both open. llm.Stream documents Close as mandatory for that reason.

# Why this file looks like llm/openai's

It is a translation between llm's types and one vendor's API, and so is its
sibling; the shape they share is the interface's and the lines that differ are
the ones a reader opened the file for. llm's documentation carries the long form
of that argument, including what was extracted instead.
*/
package anthropic
