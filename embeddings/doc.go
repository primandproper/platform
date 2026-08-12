/*
Package embeddings provides a vector embedding interface with implementations
for OpenAI, Ollama, and Cohere providers.

The three provider files are near-identical, and are left that way on purpose.
Each is a translation between this package's types and one vendor's HTTP API,
so what they share is the shape of the interface and what differs is the part a
reader opened the file for. See the llm package's documentation for the longer
form of that argument; it applies here unchanged.

What is shared is what could be got wrong twice rather than merely written
twice: [ToFloat32], because the precision decision belongs in one place, and
the observability trio each provider records through
observability/metrics.OperationSet and observability.Operation.Time.

# Where the providers differ

[Input.Purpose] is the one field a provider may act on or ignore. Cohere's
models are asymmetric and read it; openai and ollama are symmetric and do not,
and the noop embedder has no model to be either. The zero value,
[PurposeDocument], is what every provider did before the field existed, so a
caller that never sets it is unaffected — but a retrieval path embedding a
user's query against a Cohere-indexed corpus must set [PurposeQuery], because
getting that wrong degrades ranking without producing an error.
*/
package embeddings
