/*
Package openai generates vector embeddings through OpenAI's embeddings API.

Choosing it commits a deployment to an OpenAI API key, which is the only
required field, and to a network round trip per batch. Config.BaseURL is
prepended to the request path, so any endpoint that speaks OpenAI's
/v1/embeddings shape — a gateway, a proxy, a compatible vendor — can be named
instead of api.openai.com. The default model is text-embedding-3-small when
neither the request nor the config names one.

OpenAI's embedding models are symmetric — a query and a passage are embedded the
same way, and similarity between the two is the intended comparison — so this
package ignores embeddings.Input.Purpose entirely. Setting it costs nothing and
keeps the same Input portable to the cohere sibling, which does read it.

# Batching, and what a batch guarantees

GenerateEmbeddings sends every input in one request, and GenerateEmbedding is a
batch of one, so the round-trip count is the number of calls rather than the
number of texts. Results come back positionally, one per input and in order; a
response whose length does not match the request is an error rather than a
partly-filled slice. A nil input fails the whole call.

Every input in one call must resolve to the same model — the request carries one
model field. A batch spanning two is rejected rather than split, since splitting
would make the number of requests depend on how the caller happened to order its
inputs.

# Rate limits and failures

Nothing here retries. A non-200 response, 429 included, comes back as an error
carrying the status code and the body; choosing a backoff against it belongs to
the caller, who alone knows their own deadline. The platform's retry package is
the intended wrapper. A single request is bounded by Config.Timeout, or by
embeddings.DefaultRequestTimeout when that is unset.

This file is deliberately near-identical to its ollama and cohere siblings; see
embeddings and llm for why that duplication is kept.
*/
package openai
