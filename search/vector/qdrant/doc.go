// Package qdrant implements vectorsearch.Index against a Qdrant vector database
// over its REST API. Using REST avoids vendoring the Qdrant gRPC client and its
// protobuf descriptors; the surface area we need is small.
//
// Filter contract: QueryRequest.Filter is interpreted as a JSON-serializable value
// (typically map[string]any) and forwarded verbatim as the "filter" field of the
// search request body. See https://qdrant.tech/documentation/concepts/filtering/
// for the full DSL. nil means no filter.
package qdrant
