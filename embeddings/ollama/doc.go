/*
Package ollama generates vector embeddings through a running Ollama instance.

It is the one embedder here with no vendor account behind it. Config requires
nothing, no credential is read, and no Authorization header is sent — what it
commits a deployment to instead is operating an Ollama process and keeping the
model pulled into it. BaseURL defaults to http://localhost:11434, meaning a
sidecar or the developer's own machine; the default model is nomic-embed-text.

Two consequences of having nothing to validate. Construction cannot tell a
correct deployment from one pointed at nothing, so a wrong or unreachable
BaseURL is a per-call failure rather than a startup one. And because the wire is
unauthenticated, whatever reaches that address can embed — which is a statement
about the network the instance sits on, not about this package.

# Batching, and what a batch guarantees

GenerateEmbeddings sends every input in one request to /api/embed, and
GenerateEmbedding is a batch of one, so the round-trip count is the number of
calls rather than the number of texts. Results come back positionally, one per
input and in order; a response whose length does not match the request is an
error rather than a partly-filled slice. A nil input fails the whole call.

Every input in one call must resolve to the same model — the request carries one
model field. A batch spanning two is rejected rather than split, since splitting
would make the number of requests depend on how the caller happened to order its
inputs.

# Failures

Nothing here retries. A non-200 response comes back as an error carrying the
status code and the body — which, for a model that was never pulled, is where
that shows up. A single request is bounded by Config.Timeout, or by
embeddings.DefaultRequestTimeout when that is unset; local inference on a cold
model can take a while, and that ceiling is what stops a caller waiting forever.

This file is deliberately near-identical to its openai and cohere siblings; see
embeddings and llm for why that duplication is kept.
*/
package ollama
