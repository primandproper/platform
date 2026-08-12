/*
Package openai is the OpenAI-backed llm.Provider.

Choosing it commits a deployment to an OpenAI API key, which is the only
required field. Config.BaseURL is passed to the client underneath, so an
OpenAI-compatible gateway or proxy can be named instead of the vendor's own
host; Timeout bounds a request. Everything else about the request — messages,
tools, streaming, structured output — is llm's vocabulary, translated here.

Capabilities reports streaming, tools, images, reasoning, and structured output,
the same set the anthropic sibling reports. The two providers are interchangeable
at the interface, and the reason to pick one is the models behind it rather than
anything the interface exposes.

When neither the request nor Config names a model, requests go to gpt-4o-mini.

# Token accounting on streams has to be asked for

OpenAI omits usage from a streamed response unless the request opts in, so
Stream sets that option; without it a streamed llm.EventDone would carry no
Usage and a service doing all its work through Stream would have no token
numbers at all. Anthropic reports usage unconditionally and ignores the option,
which is why it is set here rather than in the shared translation.

Streamed tool calls are the other place the two vendors disagree outright —
OpenAI streams tool arguments as raw fragments, with the call's ID and name only
on the first — and llm's Stream normalizes that, so a consumer loop written
against either provider works on both.

# What is not done here

No retry. A rate limit arrives as an error matching llm.ErrRateLimited, usually
a *llm.RateLimitError carrying the provider's own advice about how long to wait;
choosing a backoff against that advice needs the caller's deadline, which this
package does not have. Wrap the call with the platform's retry package.

Stream's span and latency measurement cover the whole stream rather than the
call that opens it, so a consumer that abandons a stream without closing it
leaves both open. llm.Stream documents Close as mandatory for that reason.

# Why this file looks like llm/anthropic's

It is a translation between llm's types and one vendor's API, and so is its
sibling; the shape they share is the interface's and the lines that differ are
the ones a reader opened the file for. llm's documentation carries the long form
of that argument, including what was extracted instead.
*/
package openai
