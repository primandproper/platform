/*
Package cohere generates vector embeddings through Cohere's v2 embed API.

Choosing it commits a deployment to a Cohere API key, which is the only required
field, and to a network round trip per batch. The default model is
embed-english-v3.0 when neither the request nor the config names one — English,
which is a choice a multilingual corpus should override rather than inherit.

# Everything is embedded as a document

Cohere's v3 models take an input_type, and embed a passage differently depending
on whether it is being stored or being searched with. This package always sends
"search_document". A query embedded here therefore gets the document-side
embedding, not the query-side one, and there is no field on embeddings.Input to
say otherwise.

For a corpus that is embedded and stored, that is the right side and the
asymmetry never comes up. For retrieval where the query is embedded at request
time, it means both sides of the comparison are document embeddings — which is
self-consistent, and is not what Cohere's retrieval quality numbers describe.
That is the constraint to weigh against the openai and ollama siblings, neither
of which has a query/document distinction to get wrong.

The response is requested as float only, and narrowed to float32 by
embeddings.ToFloat32 like every other provider's.

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
the caller, who alone knows their own deadline. A single request is bounded by
Config.Timeout, or by embeddings.DefaultRequestTimeout when that is unset.

This file is deliberately near-identical to its openai and ollama siblings; see
embeddings and llm for why that duplication is kept.
*/
package cohere
