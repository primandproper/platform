// Package pgvector implements vectorsearch.Index against a PostgreSQL database
// running the pgvector extension. It uses an existing platform/database.Client for
// connection management and otelsql instrumentation.
//
// Filter contract: QueryRequest.Filter is interpreted as a string SQL fragment
// appended to the WHERE clause. Pass it as e.g. "metadata->>'kind' = 'doc'". The
// fragment is concatenated verbatim — callers are responsible for sanitizing any
// values they interpolate. Use parameter placeholders ($N) only if you also extend
// the QueryRequest with a corresponding args slice; the current shape is opaque on
// purpose.
package pgvector
