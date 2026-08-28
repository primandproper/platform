package querygen

import (
	"fmt"
	"strings"

	"github.com/primandproper/platform-go/v13/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
)

// Generator emits sqlc input for one SQL dialect.
//
// The dialect is bound to the value rather than passed to each call, because a
// fragment and the statement it lands in have to agree about which server will
// parse them. A Postgres COLLATE "C" inside MySQL is a syntax error, which is
// the good case; a Postgres ILIKE has no SQLite spelling at all and the
// substitute differs in what it folds, which is the bad one. Binding the
// dialect to the value is what makes a mixed pair unrepresentable rather than
// merely discouraged.
//
// Every method that emits SQL hangs off this type, including the ones whose
// output is currently identical on all three dialects. A caller should not have
// to know which fragments happen to be portable this week, and a divergence
// found later — the archived-row toggle was portable until sqlc's type
// inference wanted a cast — should be a change to one method body rather than a
// change to the package's surface.
type Generator struct {
	dialect dialect.Dialect
}

// For returns a Generator emitting d's SQL.
//
// It panics on a dialect outside the supported set, in the manner of the rest
// of this package: the argument is a constant in a generator binary, so an
// unsupported dialect is a typo a build should stop for rather than a condition
// a caller could do anything with. The panic value is an error wrapping
// dialect.ErrUnsupported. A caller holding a dialect that came from
// configuration rather than a literal can ask dialect.Dialect.Valid first, and
// report the rejection in whatever terms its own users understand.
func For(d dialect.Dialect) *Generator {
	if !d.Valid() {
		panic(platformerrors.Wrapf(dialect.ErrUnsupported, "querygen: dialect %q", d))
	}

	return &Generator{dialect: d}
}

// Dialect returns the dialect g emits for.
func (g *Generator) Dialect() dialect.Dialect {
	return g.dialect
}

// The expressions below are every place the three dialects genuinely disagree.
// They live together, in one file, so that adding a fourth dialect is a matter
// of reading one screen rather than grepping for casts — and so that a reader
// asking "what does this package assume about Postgres" gets a complete answer
// instead of a representative sample. They are not counted here, because a
// count in a comment is a fact that goes stale the first time one is added.
//
// Each carries the divergence and nothing else. The statements that use them are
// written once, in fragments.go, standard.go and upsert.go, and are the same
// text on every dialect apart from what these return.
//
// The last two are the exception that proves the shape of the rest: an upsert's
// conflict branch is not one statement with a substituted expression in it but
// two grammars, and they are still here rather than in upsert.go so that the
// answer to "what does this package assume about MySQL" stays one screen.

// substringMatch renders a case-insensitive substring match of column against a
// bound argument.
//
// The wildcards are concatenated around the bound value rather than the caller
// passing '%term%', because a caller assembling the pattern is a caller who can
// forget to escape a literal '%' in a user's search term, which turns a search
// for "50%" into a search for everything.
//
// Postgres has an operator for this and it is ILIKE, which folds case by the
// database's collation rules — Unicode included. Neither of the others has one.
// MySQL's LIKE is case-insensitive only because its default collation is, so a
// column declared with a _bin or _cs collation would match case-sensitively
// while the emitted SQL still said LIKE; SQLite's LIKE folds ASCII only, and
// only while PRAGMA case_sensitive_like is off. Both are made unconditional by
// folding both sides explicitly, which costs the index either way — as ILIKE
// does on Postgres without a trigram index.
//
// The residual difference is worth stating plainly rather than papering over:
// on Postgres this matches "STRASSE" against "straße" and on the other two it
// does not, because LOWER outside Postgres folds ASCII and stops.
//
// Neither non-Postgres arm casts its argument, and on MySQL that is load-bearing
// rather than an omission. A bound parameter is the weakest thing in MySQL's
// coercibility order, so it takes the collation of whatever it is compared
// against; CAST(... AS CHAR) turns it into a string carrying the connection's
// own collation instead, and comparing that against a column whose collation
// differs is not a fallback but error 1267, "illegal mix of collations". The
// connection's collation is the driver's to choose and the column's is the
// schema's, so the two disagreeing is the ordinary case rather than the exotic
// one — a go-sql-driver connection against a MySQL 8 table disagrees out of the
// box. Leaving the parameter uncast is what lets it adopt the column's.
func (g *Generator) substringMatch(column, argument string) string {
	switch g.dialect {
	case dialect.MySQL:
		return fmt.Sprintf("LOWER(%s) LIKE CONCAT('%%', LOWER(sqlc.arg(%s)), '%%')", column, argument)
	case dialect.SQLite:
		return fmt.Sprintf("LOWER(%s) LIKE '%%' || LOWER(sqlc.arg(%s)) || '%%'", column, argument)
	// Postgres, which For has already narrowed the alternatives to.
	default:
		return fmt.Sprintf("%s ILIKE '%%' || sqlc.arg(%s)::text || '%%'", column, argument)
	}
}

// prefixPatternArgument renders the bound side of a prefix match: the argument
// carrying the pattern, plus whatever the dialect's analyzer needs in order to
// give it a type.
//
// Postgres needs the cast, and for a reason worth stating rather than
// rediscovering. It desugars `x LIKE p ESCAPE e` into a call to like_escape,
// which is overloaded on text and on bytea; an untyped parameter resolves to
// the bytea arm, so the generated Go field for the pattern comes back []byte
// while every other consumer of that column has a string. The cast picks the
// arm the column is actually in. It is the same cast substringMatch applies for
// the same reason.
//
// The other two need nothing, and on MySQL the absence is load-bearing rather
// than an omission — a bound parameter is the weakest thing in its
// coercibility order, so it adopts the column's collation, and a cast would
// give it the connection's instead. substringMatch carries the long form.
func (g *Generator) prefixPatternArgument(argument string) string {
	if g.dialect == dialect.Postgres {
		return fmt.Sprintf("sqlc.arg(%s)::text", argument)
	}

	return fmt.Sprintf("sqlc.arg(%s)", argument)
}

// byteOrdered wraps an expression so that comparing and ordering it is a
// comparison of bytes rather than of collated text.
//
// search/sync requires ascending byte order from the reindex scan, because the
// pruning half of a reindex merges that stream against the index's own stream of
// ids. Two ordered streams merged under disagreeing orders do not fail; they
// conclude that live documents are absent from the source and delete them. Every
// dialect here defaults to something other than byte order for text — Postgres
// to the database's collation, which under en_US.UTF-8 sorts case-insensitively
// and ignores punctuation; MySQL to utf8mb4_0900_ai_ci, which does the same — so
// the order is named rather than assumed on all three, SQLite included, where it
// happens to already be the default.
//
// MySQL is a cast rather than a COLLATE clause on purpose. utf8mb4_bin would be
// the direct translation and it is wrong for a column that is not utf8mb4: a
// latin1 id column would take the collation clause and fail at parse time, for a
// reason that reads as a charset problem. Casting to BINARY compares the stored
// bytes whatever they are, which is the property being asked for.
func (g *Generator) byteOrdered(expression string) string {
	switch g.dialect {
	case dialect.MySQL:
		return fmt.Sprintf("CAST(%s AS BINARY)", expression)
	case dialect.SQLite:
		return expression + " COLLATE BINARY"
	// Postgres, which For has already narrowed the alternatives to.
	default:
		return expression + ` COLLATE "C"`
	}
}

// timeHorizon renders the timestamp an unset filter bound coalesces to: 999
// years either side of now, per sign, which is "-" for a lower bound and "+"
// for an upper one.
//
// A bound that is absent is rendered as a bound that cannot exclude anything,
// rather than as an omitted predicate, so that all four bounds are the same
// statement whichever subset of them a caller sent — see boundPredicate.
//
// 999 years is inside every dialect's representable range and is not close to
// any of their edges: MySQL's DATETIME stops at 9999-12-31 and starts at
// 1000-01-01, which the offset clears in both directions for any date this code
// will run on. The scalar subquery is not required by any of them; it is kept
// because it is what Postgres's sqlc reads the COALESCE's type through, and one
// shape across three dialects is one shape to get wrong.
//
// SQLite has no interval type and no arithmetic on timestamps, so the offset is
// a modifier string handed to datetime(). Its result is text in the same
// YYYY-MM-DD HH:MM:SS shape CURRENT_TIMESTAMP produces, which is what makes the
// comparison against it lexicographic and correct — and what makes a SQLite
// table storing timestamps in any other shape a table this package cannot
// filter. See the package comment.
func (g *Generator) timeHorizon(sign string) string {
	switch g.dialect {
	case dialect.MySQL:
		return fmt.Sprintf("(SELECT %s %s INTERVAL 999 YEAR)", NowExpression, sign)
	case dialect.SQLite:
		return fmt.Sprintf("(SELECT datetime(%s, '%s999 years'))", NowExpression, sign)
	// Postgres, which For has already narrowed the alternatives to.
	default:
		return fmt.Sprintf("(SELECT %s %s '999 years'::INTERVAL)", NowExpression, sign)
	}
}

// storedNow renders the current time as a statement should store it, which is
// not always how [NowExpression] asks for it.
//
// MySQL's bare CURRENT_TIMESTAMP is second-granular regardless of the column's
// declared precision, so a DATETIME(6) assigned from it holds a whole second and
// two updates inside one second write the same value. That is not a cosmetic
// loss of precision. MySQL's affected-row count reports rows *changed* rather
// than rows matched, so an update whose columns all already hold their new
// values — a form saved twice, a reconciler writing what it read — changes
// nothing, reports zero, and reaches a caller that reads zero rows as "the row
// is not there". The failure is a not-found error for a row that exists, on one
// dialect, for a write that was correct.
//
// CURRENT_TIMESTAMP(6) is the fractional form, and it is also what a DATETIME(6)
// column's own DEFAULT has to name for the same reason — so the statement and
// the schema ask for the time the same way.
//
// Postgres and SQLite need nothing: Postgres's CURRENT_TIMESTAMP is microsecond
// resolution already, and SQLite has no fractional form to ask for — its
// timestamps are the second-granular text its comparisons are lexicographic
// over.
func (g *Generator) storedNow() string {
	if g.dialect == dialect.MySQL {
		return NowExpression + "(6)"
	}

	return NowExpression
}

// includeArchivedFlag renders the archived toggle's argument: a nullable
// boolean coalesced to false.
//
// Each dialect needs something appended, and for the same reason: sqlc types
// this argument from its use, and its use here is a bare predicate with no
// column beside it to take a type from. What differs is what each of them
// accepts as the thing that supplies one.
//
// Postgres takes a cast. COALESCE over an untyped NULL leaves it to guess, and
// ::boolean is what makes the generated Go field a *bool rather than an
// interface{} the caller has to convince.
//
// MySQL and SQLite have no boolean type to cast to — both spell it as an
// integer — so the nearest cast (CAST(... AS UNSIGNED)) would only turn the
// generated field into a number. What they take instead is a comparison: the
// coalesced value against a literal, which supplies the type from the literal.
//
// Both of them need it, and both report its absence somewhere else entirely. A
// list query's counts are scalar subqueries in the SELECT list, and an argument
// neither analyzer can type inside one makes it lose the subquery's alias — so
// `sqlc compile` reports `column "filtered_count" does not exist` against a
// line whose alias is right there.
//
// The comparison changes no semantics on either: both spell true as 1, a bound
// Go bool arrives as 1 or 0, and an absent flag coalesces to false and compares
// unequal.
func (g *Generator) includeArchivedFlag() string {
	coalesced := fmt.Sprintf("COALESCE(sqlc.narg(%s), false)", IncludeArchivedArg)

	if g.dialect == dialect.Postgres {
		return coalesced + "::boolean"
	}

	return coalesced + " = true"
}

// limitClause renders the page-size clause a keyset walk ends on.
//
// Postgres and SQLite take an expression, so an absent page size coalesces to
// filtering.DefaultQueryFilterLimit and the generated Go parameter is a pointer
// the caller may leave nil. MySQL takes an integer literal or a bare placeholder
// after LIMIT and nothing else — COALESCE there is a parse error, not a slower
// plan, and so is a named argument reference — so its clause binds the size and
// spells the marker directly.
//
// That bare marker is the one argument reference in this package that carries no
// name, because MySQL's grammar has nowhere to put one. Everything downstream
// still knows which argument it is: bindArguments records it under LimitArg like
// any other, so a caller binds the page size under the same key on all three
// dialects. What a MySQL consumer generating Go from these files gets is a
// parameter sqlc named from the clause rather than from the reference.
//
// This is the one place a dialect changes a generated signature rather than only
// the SQL behind it, and leveling the other two down to match would be the
// wrong trade: it would take a working default away from the dialects that can
// express one in order to make a limitation uniform. Nothing drifts by leaving
// them different, because the default is filtering's constant rather than a
// number written here — the SQL and filtering.QueryFilter.Normalize read the
// same one.
//
// What a MySQL consumer owes its queries, then, is Normalize: it turns an absent
// or zero page size into that same constant and clamps an oversized one, which
// is the treatment the URL parameter already gets. A MySQL query handed a zero
// returns no rows, which is loud, rather than a page of some other size.
func (g *Generator) limitClause() string {
	if g.dialect == dialect.MySQL {
		return "LIMIT " + g.dialect.Placeholder(1)
	}

	return fmt.Sprintf("LIMIT COALESCE(sqlc.narg(%s), %d)", LimitArg, filtering.DefaultQueryFilterLimit)
}

// setPredicate matches a column against a bound set of values.
//
// Postgres has arrays and takes the whole set as one argument, which is what
// keeps a flush of a hundred ids a statement with one parameter rather than a
// hundred. The other two have no array type, so the set is expanded by sqlc into
// as many placeholders as there are values — sqlc.slice, which sqlc documents
// for exactly these two engines. The generated Go signature is []string either
// way, so this is a difference in what reaches the server rather than in what a
// caller writes.
//
// The Postgres cast is to text[], which makes the set a set of text on every
// dialect. That is this module's key convention rather than a decision taken
// here — ids are xids and natural keys are strings — and it is what
// Generator.SetReadQuery documents to its callers.
//
// The column is a parameter because two statements key on a set and they key on
// different columns: the bulk stamp keys on the row's own id, and a batched read
// keys on whatever the child rows hang from. One rendering, so the two cannot
// come to disagree about how a set reaches a server.
func (g *Generator) setPredicate(column, argument string) string {
	if g.dialect == dialect.Postgres {
		return fmt.Sprintf("%s = ANY(sqlc.arg(%s)::text[])", column, argument)
	}

	return fmt.Sprintf("%s IN (sqlc.slice(%s))", column, argument)
}

// conflictHeader opens an upsert's conflict branch: everything between the
// INSERT and the assignments the colliding row receives.
//
// This is the one place the three dialects disagree about a statement's shape
// rather than about an expression inside one, and the disagreement is not
// cosmetic. Postgres and SQLite take a conflict target — the columns of the
// unique index the collision is detected on — and act only on that index; MySQL
// has no such clause and fires on whichever unique key was violated, primary key
// included.
//
// So the target is written where it can be written and the difference is
// documented where it cannot. What follows from it is in upsertStatement: the
// key's own columns are never assigned, because on MySQL a collision may have
// been on some other key and an assignment would then move the row rather than
// restate it.
func (g *Generator) conflictHeader(keyColumns []string) string {
	if g.dialect == dialect.MySQL {
		return "ON DUPLICATE KEY UPDATE"
	}

	// Postgres and SQLite, which For has already narrowed the alternatives to,
	// spell the target and the SET the same way.
	return fmt.Sprintf("ON CONFLICT (%s) DO UPDATE SET", strings.Join(keyColumns, ", "))
}

// insertedValue names, inside a conflict branch, the value the INSERT that
// collided supplied for column — as against the value already in the row.
//
// Postgres and SQLite expose the rejected row under the EXCLUDED alias. MySQL
// has no such alias and spells it VALUES(column), which reads as the VALUES
// clause and is not one.
//
// MySQL 8.0.19 added a row alias (`... AS new` before the ON DUPLICATE clause)
// and deprecated this spelling, and it is still the spelling emitted here: the
// alias form is a parse error on 8.0.18 and earlier, and MariaDB — which speaks
// the rest of this dialect — has no version that accepts it. A deprecation
// warning on the newest server is a cheaper failure than a syntax error on
// every older one.
func (g *Generator) insertedValue(column string) string {
	if g.dialect == dialect.MySQL {
		return fmt.Sprintf("VALUES(%s)", column)
	}

	return "EXCLUDED." + column
}
