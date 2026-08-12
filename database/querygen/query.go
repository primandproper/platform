package querygen

import (
	"fmt"
	"strings"
)

// QueryType is the sqlc annotation suffix declaring what a query returns. It is
// the half of the annotation sqlc reads to decide the generated method's
// signature, so a mismatch between it and the SQL is a compile error in the
// generated package rather than a runtime surprise.
type QueryType string

const (
	// ExecType returns nothing. It is the annotation for an INSERT whose caller
	// does not need to know whether a row was written, because a failed one
	// raises rather than returning zero.
	ExecType QueryType = ":exec"
	// ExecRowsType returns the number of rows affected. It is the annotation for
	// the writes whose row count is the answer — an UPDATE or an archival that
	// matched nothing is how a caller learns the row was already gone.
	ExecRowsType QueryType = ":execrows"
	// ManyType returns a slice of rows.
	ManyType QueryType = ":many"
	// OneType returns exactly one row, and an error when there is none.
	OneType QueryType = ":one"
)

// QueryAnnotation is the `-- name: X :one` line sqlc reads above a query. Name
// becomes the generated Go method's name, so it has to be unique across every
// file in a sqlc package, not merely within its own file.
type QueryAnnotation struct {
	Name string
	Type QueryType
}

// Query is one annotated statement: the SQL, and the annotation that tells sqlc
// what to make of it.
type Query struct {
	Content    string
	Annotation QueryAnnotation
}

// Render returns the query as sqlc reads it — the annotation comment, then the
// statement, terminated.
//
// The terminator is appended only when the content lacks one, so a statement
// that already ends in a semicolon does not acquire a second, empty one.
func (q *Query) Render() string {
	content := q.Content
	if !strings.HasSuffix(content, ";") {
		content += ";"
	}

	return fmt.Sprintf("-- name: %s %s\n%s\n", q.Annotation.Name, q.Annotation.Type, content)
}

// RenderFile assembles queries into the byte-exact contents of one sqlc input
// file: each rendered query, one blank line between them, one trailing newline.
//
// Trailing whitespace is stripped from every line. That is not cosmetic. A
// generator is usually run twice — once to write the files and once, in CI, to
// check that the committed files still match — and the check is a byte
// comparison. Composing fragments into a statement is exactly the operation that
// leaves a stray space at the end of a line, so normalizing here is what keeps
// the check answering a question about SQL rather than about whitespace.
func RenderFile(queries []*Query) string {
	if len(queries) == 0 {
		return ""
	}

	// Each render already ends in a newline, so joining on one more is what puts
	// a blank line between statements.
	rendered := make([]string, 0, len(queries))
	for _, query := range queries {
		rendered = append(rendered, query.Render())
	}

	var lines []string
	for line := range strings.SplitSeq(strings.TrimSpace(strings.Join(rendered, "\n")), "\n") {
		lines = append(lines, strings.TrimRight(line, " \t"))
	}

	return strings.Join(lines, "\n") + "\n"
}
