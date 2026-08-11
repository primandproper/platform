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
*/
package embeddings
