/*
Package dialect names the SQL dialects the module's SQL-emitting packages
support, and carries the small helpers every one of them otherwise reimplements:
bind-marker rendering, identifier vetting, and DDL statement splitting.

It exists to be a leaf. database/migrate, outbox, and authorization/database all
speak the same three dialects, and their migrations subpackages cannot import
their parents without closing a cycle through the parents' tests — so before
this package, each of the five declared its own Dialect type and tests converted
between them. One shared type makes those conversions unrepresentable.
*/
package dialect

import (
	"regexp"
	"strconv"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v8/errors"
)

// Dialect selects the SQL a package emits. It must match the database provider
// the emitted SQL runs against.
type Dialect string

const (
	// Postgres targets PostgreSQL, which numbers its placeholders and supports
	// SKIP LOCKED.
	Postgres Dialect = "postgres"
	// MySQL targets MySQL 8.0+ — the first version with WITH RECURSIVE — which
	// supports SKIP LOCKED.
	MySQL Dialect = "mysql"
	// SQLite targets SQLite, which is single-writer by nature and has no
	// SKIP LOCKED.
	SQLite Dialect = "sqlite"
)

// ErrUnsupported indicates a dialect outside the supported set. Packages wrap
// it with their own context, so errors.Is works across all of them.
var ErrUnsupported = platformerrors.New("unsupported SQL dialect")

// Valid reports whether d is a dialect this module can emit SQL for.
func (d Dialect) Valid() bool {
	switch d {
	case Postgres, MySQL, SQLite:
		return true
	default:
		return false
	}
}

// SupportsSkipLocked reports whether the dialect can claim rows with
// FOR UPDATE SKIP LOCKED, which is what allows more than one competing worker
// to claim from the same table at once.
func (d Dialect) SupportsSkipLocked() bool {
	return d == Postgres || d == MySQL
}

// Placeholder renders the n-th bind marker (1-indexed). Postgres numbers its
// placeholders; MySQL and SQLite do not.
func (d Dialect) Placeholder(n int) string {
	if d == Postgres {
		return "$" + strconv.Itoa(n)
	}

	return "?"
}

// Placeholders renders count bind markers starting at start, joined for use
// inside an IN clause or a VALUES tuple.
func (d Dialect) Placeholders(start, count int) string {
	parts := make([]string, 0, count)
	for i := range count {
		parts = append(parts, d.Placeholder(start+i))
	}

	return strings.Join(parts, ", ")
}

// identifier matches a name safe to interpolate into query text: a bare
// identifier, optionally qualified by exactly one schema. ASCII only, and
// anchored at both ends. Go's regexp is RE2, where $ means end of text rather
// than "before an optional trailing newline" as in Perl, so there is no
// trailing-newline escape from the anchor.
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// ErrInvalidIdentifier indicates a name that ValidIdentifier rejects. Packages
// wrap it with their own context, so errors.Is works across all of them —
// including across a package that builds a table's DDL and one that queries it,
// which is the pair most likely to be checked against each other.
var ErrInvalidIdentifier = platformerrors.New("invalid SQL identifier")

// ValidIdentifier reports whether s is safe to interpolate into query text as
// a table name. Table names are interpolated rather than bound, so they are
// restricted rather than escaped.
func ValidIdentifier(s string) bool {
	return identifier.MatchString(s)
}

// SplitStatements strips '--' comments from ddl and splits it into individually
// executable statements on ';', preserving statement order.
//
// Comments come out before the split, not after. A '--' comment may contain a
// semicolon — prose routinely does — and splitting first tears such a comment
// in half, leaving its tail masquerading as SQL at the head of the next
// statement.
//
// Comment stripping handles whole-line '--' comments and blank lines only, not
// a '--' appearing after SQL on the same line, nor semicolons inside string
// literals; the DDL shipped by this module contains neither, and the round-trip
// tests against real servers are what keep that true.
func SplitStatements(ddl string) []string {
	var stmts []string
	for raw := range strings.SplitSeq(stripComments(ddl), ";") {
		if stmt := strings.TrimSpace(raw); stmt != "" {
			stmts = append(stmts, stmt)
		}
	}

	return stmts
}

// stripComments drops whole-line '--' comments and blank lines.
func stripComments(ddl string) string {
	var kept []string

	for line := range strings.SplitSeq(ddl, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "--") {
			kept = append(kept, line)
		}
	}

	return strings.Join(kept, "\n")
}
