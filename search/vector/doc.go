// Package vectorsearch provides a generic interface for vector (nearest-neighbor)
// search backends, parallel to the textsearch package under search/text. Each provider
// implementation lives in a subpackage and is selected at runtime via the dispatch
// config under search/vector/config.
//
// The atom is intentionally narrow: upsert, query, delete, and wipe. Index management
// (creation, dimension, distance metric) is handled at construction time via each
// provider's Config. Cross-provider filter portability is explicitly out of scope —
// QueryRequest.Filter is opaque any, interpreted per-provider, and callers that need
// portability are expected to translate at the call site.
//
// Higher-level concerns such as retrieval-augmented generation, hybrid sparse+dense
// search, and reranking pipelines are compositions of this atom with embeddings, llm,
// and search/text and are out of scope for the platform library.
package vectorsearch
