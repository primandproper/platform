package querygen

import (
	"fmt"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v12/database"
	"github.com/primandproper/platform-go/v12/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v12/errors"
	"github.com/primandproper/platform-go/v12/filtering"
)

// A statement in this package names its arguments — created_after, cursor,
// result_limit — and something has to turn a name into the spelling the
// consumer parses. sqlc reads `sqlc.arg(created_after)` and generates a struct
// field from it; a database driver reads `$3` and takes a positional slice. The
// SQL either side of that spelling is the same SQL, and it is the part with the
// semantics in it: which bound admits NULL, whether the archived flag gates the
// exclusion or admits rows, that the cursor predicate is absent from the counts.
//
// Those semantics are the kind that can be wrong twice — a runtime renderer that
// wrote its own archived predicate the other way round would behave correctly
// until someone removed a redundant IS NULL elsewhere, and nothing would tie the
// two renderings together. So the argument spelling is the seam, and everything
// above it is written once: the same fragments.go and standard.go produce sqlc
// input and executable SQL, and the pair cannot drift because there is no pair.

// binder renders an argument reference. The two implementations are the two
// consumers of this package: sqlc, which reads named references out of the text,
// and a database driver, which reads placeholders and takes its values
// positionally.
type binder interface {
	// arg renders a reference to a required argument.
	arg(name string) string
	// narg renders a reference to an argument that may be NULL. sqlc reads the
	// distinction and generates a nullable Go field; a driver does not, and
	// binds whatever it was handed.
	narg(name string) string
	// slice renders a reference to a set of values expanded into the statement.
	// Only the dialects without an array type reach it — see idSetPredicate.
	slice(name string) string
}

// sqlcBinder renders the named references sqlc reads. It holds nothing: the same
// name renders the same text wherever it appears, which is what lets one
// predicate be rendered into a SELECT list and again into two count subqueries
// beside it.
type sqlcBinder struct{}

func (sqlcBinder) arg(name string) string   { return fmt.Sprintf("sqlc.arg(%s)", name) }
func (sqlcBinder) narg(name string) string  { return fmt.Sprintf("sqlc.narg(%s)", name) }
func (sqlcBinder) slice(name string) string { return fmt.Sprintf("sqlc.slice(%s)", name) }

// boundSentinel brackets an argument name in a partly-rendered statement, until
// resolve turns it into a bind marker.
//
// NUL appears in no SQL this package emits and in no identifier
// dialect.ValidIdentifier accepts, and it never reaches a server: a Bound does
// not exist until resolve has replaced every one of them.
const boundSentinel = "\x00"

// boundBinder renders an argument reference as its name in sentinels rather than
// as a placeholder. resolve then numbers the sentinels in one pass over the
// finished statement, which is where a Bound's SQL and its argument order both
// come from.
//
// The indirection is the whole of what makes this correct, and it is worth being
// explicit about why a binder that handed out ordinals as it rendered is not.
//
// A fragment here is rendered once and spliced more than once. filterPredicates
// goes into the SELECT's WHERE and into both count subqueries, so created_after
// appears three times in a list query from one rendering of it, and a match
// column does the same. A render-time binder would give that one rendering one
// placeholder and record one argument while the statement carried three markers
// — on Postgres a harmless renumbering, and on the positional dialects a driver
// handed a cursor where it wanted a limit. It would also make the argument order
// depend on the order Go happened to evaluate the arguments of the Sprintf that
// assembled the statement, which is not the order they appear in the text.
//
// Numbering the finished text has neither problem: a placeholder is numbered
// where it appears, however it got there. It is also exactly what sqlc does to
// the other rendering of these same statements, which is what lets the two be
// compared statement for statement.
//
// What differs per dialect is only whether a repeat is one argument or several.
// Postgres numbers its placeholders, so repeats reuse the first occurrence's
// number and the value is bound once. MySQL's and SQLite's are positional — a
// bare `?` takes the next value and there is no way to name an earlier one — so
// there each occurrence appends the name again and the caller binds the value as
// many times as it appears. Both are correct; only the count differs, and
// Bound.Args reports whichever one this dialect produced.
type boundBinder struct {
	dialect dialect.Dialect
}

// numbered reports whether this dialect's placeholders can name an earlier
// argument, which decides whether a repeated name is bound once or once per
// occurrence.
//
// The split is dialect.Placeholder's, not this package's: it renders `$n` for
// Postgres and `?` for the other two. SQLite's own syntax does have a numbered
// form (`?NNN`), but nothing in this module emits it, and treating SQLite as
// numbered here would collapse a repeat's arguments while leaving its markers in
// place — a statement that binds every value after the first into the following
// slot rather than one that fails to parse.
func (b boundBinder) numbered() bool {
	return b.dialect == dialect.Postgres
}

func (boundBinder) arg(name string) string  { return boundSentinel + name + boundSentinel }
func (boundBinder) narg(name string) string { return boundSentinel + name + boundSentinel }

// resolve replaces every sentinel in statement with this dialect's bind marker,
// returning the statement a driver takes and the argument each marker stands
// for, in order.
func (b boundBinder) resolve(statement string) (sql string, args []string) {
	// Sentinels are written in pairs, so a well-formed statement always splits
	// into an odd number of parts. An even one means an unterminated sentinel —
	// reachable only through an identifier carrying a NUL, which
	// dialect.ValidIdentifier rejects — and everything below would read the SQL
	// either side of it as an argument name and shift every argument after it.
	parts := strings.Split(statement, boundSentinel)
	if len(parts)%2 == 0 {
		panic(platformerrors.Wrap(ErrUnboundableStatement, "querygen: unterminated argument reference"))
	}

	ordinals := make(map[string]int, len(parts)/2)

	var out strings.Builder

	for i, part := range parts {
		// The even parts are SQL and the odd ones are argument names, because
		// the sentinels they were split on come in pairs.
		if i%2 == 0 {
			out.WriteString(part)

			continue
		}

		if n, ok := ordinals[part]; ok && b.numbered() {
			out.WriteString(b.dialect.Placeholder(n))

			continue
		}

		args = append(args, part)
		ordinals[part] = len(args)
		out.WriteString(b.dialect.Placeholder(len(args)))
	}

	return out.String(), args
}

// slice is unreachable: idSetPredicate is the only caller and it takes the array
// arm on Postgres, while the dialects that would reach this arm expand a set
// into a placeholder per element, whose count is not known until the values are.
// A bound statement wanting an id set therefore has to be rendered against the
// set it will bind, which BoundIDSet does; reaching here means a new caller
// arrived without one.
func (boundBinder) slice(name string) string {
	panic(platformerrors.Wrapf(ErrUnboundableStatement, "querygen: argument %q is a set", name))
}

// ErrUnboundableStatement indicates a statement that has no executable
// rendering, because the number of placeholders it needs is not known until the
// values are.
var ErrUnboundableStatement = platformerrors.New("statement cannot be rendered as bound SQL")

// Bound is one statement rendered for a database driver: the SQL, and the names
// of the arguments its placeholders stand for, in the order the driver takes
// them.
//
// Args holds names rather than values because the statement is rendered once,
// at construction, and executed many times. A caller keeps the Bound and calls
// Bind per execution.
type Bound struct {
	// SQL is the statement, with this dialect's placeholders in it.
	SQL string
	// Args names each placeholder's argument, in positional order. A name
	// repeats on the positional dialects, once per occurrence — see
	// boundBinder.
	Args []string
}

// ErrUnboundArgument indicates a statement executed without a value for one of
// the arguments it names. It is a programming error rather than a caller's:
// nothing on a request path chooses which arguments a statement has.
var ErrUnboundArgument = platformerrors.New("no value supplied for a statement argument")

// Bind assembles the positional argument slice this statement takes from a map
// of values keyed by argument name.
//
// A missing name is an error rather than a nil, because a nil is a legitimate
// value for every nullable argument here and the two are indistinguishable once
// bound. A statement whose created_after is genuinely absent binds an explicit
// nil under that key.
func (b Bound) Bind(values map[string]any) ([]any, error) {
	args := make([]any, 0, len(b.Args))

	for _, name := range b.Args {
		value, ok := values[name]
		if !ok {
			return nil, platformerrors.Wrapf(ErrUnboundArgument, "argument %q", name)
		}

		args = append(args, value)
	}

	return args, nil
}

// bound renders one statement through a positional binder and resolves it.
//
// It is the only place the binder is swapped, so nothing renders half a
// statement through one binder and half through another, and the only place a
// statement's arguments are numbered — which is why every Bound* method below is
// a closure handed to this rather than an assembly of its own.
//
// The swap is onto a copy. A Generator is held for the lifetime of a store and
// asked for sqlc text and bound text by turns, so a binder installed on the
// original would leave a generator binary emitting $1 into the .sql files it
// wrote next.
func (g *Generator) bound(render func(*Generator) string) Bound {
	b := boundBinder{dialect: g.dialect}

	sql, args := b.resolve(render(&Generator{bind: b, dialect: g.dialect}))

	return Bound{SQL: sql, Args: args}
}

// BoundIDSet renders the id-set predicate for a known number of ids, along with
// the argument names each placeholder stands for.
//
// It is separate from the rest because it is the one predicate whose placeholder
// count depends on the values. Postgres takes the set as one array argument and
// so renders the same text whatever the count; the others expand it to one
// placeholder per element, and a statement rendered for three ids cannot be
// executed with four. That is the same split idSetPredicate makes for the sqlc
// path, where sqlc.slice is the macro doing the expanding — which is also why
// this is a method rather than something boundBinder.slice could render: the
// arity is the caller's and a binder is not told it.
//
// The placeholders it renders start at 1, so the set is the leading argument of
// the statement it is spliced into — which for the read this exists for, "fetch
// these ids", is the only argument. A caller putting it after another bound
// argument renders a statement whose numbering starts over.
//
// Postgres matches with ANY over the bound array rather than with `=`, which
// compares a text column against a text[] and is an error rather than an empty
// result. The names of the expanded arguments carry a '#', which no identifier
// dialect.ValidIdentifier accepts contains, so an element cannot collide with a
// column bound in the same statement.
func (g *Generator) BoundIDSet(table string, count int) (predicate string, args []string) {
	id := Qualify(table, IDColumn)

	if g.dialect == dialect.Postgres {
		return fmt.Sprintf("%s = ANY(%s::text[])", id, g.dialect.Placeholder(1)), []string{IDsArg}
	}

	args = make([]string, 0, count)
	for i := range count {
		args = append(args, fmt.Sprintf("%s#%d", IDsArg, i))
	}

	return fmt.Sprintf("%s IN (%s)", id, g.dialect.Placeholders(1, count)), args
}

// Match is an equality predicate on one column, for a read keyed on something
// other than the row's own id — comments on one reference, signups for one
// waitlist.
//
// It is a column name rather than rendered SQL because the statements it lands
// in render it more than once: a list query carries its predicates in the SELECT
// and again in each of the two count subqueries beside it. A caller handing over
// finished SQL would have to know how many times its placeholder was about to
// appear, which on Postgres is once and on the positional dialects is three
// times. Handing over the column instead leaves that to the binder, which is the
// thing that knows.
type Match struct {
	// Column is the column matched. It is bound, never interpolated, so its
	// value needs no escaping; the name itself is interpolated and is therefore
	// restricted — see dialect.ValidIdentifier.
	Column string
}

// BoundList renders a list query carrying extra equality predicates.
//
// The filter window, the archived toggle, the cursor and the two counts are the
// same ones StandardCRUD's list query gets, from the same fragments, so a
// keyed read filters exactly as an unkeyed one does.
//
// Each match binds under its own column name, so a caller assembles the argument
// map by column and Bind puts the values where this dialect wants them.
func (g *Generator) BoundList(table string, columns []string, matches ...Match) Bound {
	return g.bound(func(gg *Generator) string {
		all := gg.matchPredicates(table, true, matches)

		return fmt.Sprintf("SELECT\n\t%s,\n\t%s,\n\t%s\nFROM %s\nWHERE %s\n%s;",
			strings.Join(QualifyAll(table, columns), ",\n\t"),
			gg.FilterCountSelect(table, columns, nil, all...),
			gg.TotalCountSelect(table, columns, nil, all...),
			table,
			gg.FilterConditions(table, columns, all...),
			gg.CursorLimitClause(table),
		)
	})
}

// BoundArchiveMatching renders the bulk archival a cascade needs: soft-delete
// every unarchived row matching the given equality predicates.
//
// It has no id predicate, which is the point — this is the statement for
// "archive the comments on this reference", not "archive this comment". A caller
// that passes no matches gets a statement that archives the table, so the empty
// set is refused rather than rendered.
func (g *Generator) BoundArchiveMatching(table string, matches ...Match) (Bound, error) {
	if len(matches) == 0 {
		return Bound{}, platformerrors.Wrap(ErrUnboundableStatement, "querygen: archive with no matches would archive the table")
	}

	return g.bound(func(gg *Generator) string {
		// Unqualified: an UPDATE's WHERE carries no table qualifier, for the
		// same reason singleRowPredicates drops one.
		predicates := append(
			[]string{fmt.Sprintf("%s IS NULL", ArchivedAtColumn)},
			gg.matchPredicates(table, false, matches)...,
		)

		return fmt.Sprintf("UPDATE %s SET\n\t%s = %s\nWHERE %s;",
			table,
			ArchivedAtColumn, NowExpression,
			joinPredicates(predicates, "\t"),
		)
	}), nil
}

// The six Bound* statement builders below are the executable counterparts of
// what StandardCRUD emits, and they are deliberately one per statement rather
// than one call returning the set.
//
// StandardCRUD answers a generator binary asking "what queries does this table
// need", where the set is the unit and a table gets all of it. A runtime store
// asks something narrower and per-statement: this table's reads are open within
// its scope but only its owner may write, so the get names one predicate column
// and the update names two. Expressed as one call over one options struct that
// would be a set of per-query overrides; expressed as six calls it is six
// argument lists.
//
// Each takes the extra predicate columns as Match values, and the row's own id
// is one of them where it applies rather than a special case — which is what
// lets a caller add a tenancy scope column without this package knowing what a
// tenancy scope is.

// BoundGet renders the read of one row by id, plus any extra predicate columns.
func (g *Generator) BoundGet(table string, columns []string, extra ...Match) Bound {
	return g.bound(func(gg *Generator) string {
		return gg.getStatement(table, columns, "", extra...)
	})
}

// BoundExists renders the existence check for one row by id, plus any extra
// predicate columns. It reports what BoundGet would find without reading it.
func (g *Generator) BoundExists(table string, columns []string, extra ...Match) Bound {
	return g.bound(func(gg *Generator) string {
		return gg.existsStatement(table, columns, "", extra...)
	})
}

// BoundCreate renders the insert. insertColumns is what the caller supplies —
// ForInsert over the table's columns — and nullable names those whose value may
// be NULL.
func (g *Generator) BoundCreate(table string, insertColumns, nullable []string) Bound {
	return g.bound(func(gg *Generator) string {
		return gg.createStatement(table, insertColumns, nullable)
	})
}

// BoundUpdate renders the update: every mutable column assigned, last_updated_at
// stamped, keyed on the id and any extra predicate columns.
func (g *Generator) BoundUpdate(table string, columns, updateColumns, nullable []string, extra ...Match) Bound {
	return g.bound(func(gg *Generator) string {
		return gg.updateStatement(table, columns, updateColumns, "", nullable, extra...)
	})
}

// BoundArchive renders the soft delete of one row by id, plus any extra
// predicate columns.
func (g *Generator) BoundArchive(table string, extra ...Match) Bound {
	return g.bound(func(gg *Generator) string {
		return gg.archiveStatement(table, "", extra...)
	})
}

// BindFilter writes a filtering.QueryFilter's values into an argument map under
// the names the emitted statements bind them by.
//
// It is here rather than in filtering because these names are this package's:
// filtering owns the struct and the URL parameters, and the mapping between
// those and the SQL arguments is what this package emits. A caller assembling
// its own map would be a second copy of that mapping, and a second copy that
// spelled created_after "createdAfter" would bind nothing and filter nothing —
// which looks exactly like a filter nobody set.
//
// It hangs off the Generator rather than standing alone because a time is not
// one value on all three servers — see filterTime. Everything else it binds is,
// and is bound through database's null helpers so that an unset field reaches
// the server as the NULL the emitted COALESCE expects rather than as a zero.
//
// A nil filter binds the defaults, so a caller that took none still produces a
// bindable statement. It writes only the arguments it owns: a keyed read's match
// columns go into the same map, and a filter that cleared them would be a read
// that lost its key.
func (g *Generator) BindFilter(values map[string]any, filter *filtering.QueryFilter) {
	if filter == nil {
		filter = filtering.DefaultQueryFilter()
	}

	values[CreatedAfterArg] = g.filterTime(filter.CreatedAfter)
	values[CreatedBeforeArg] = g.filterTime(filter.CreatedBefore)
	values[UpdatedAfterArg] = g.filterTime(filter.UpdatedAfter)
	values[UpdatedBeforeArg] = g.filterTime(filter.UpdatedBefore)
	values[IncludeArchivedArg] = database.NullBoolFromBoolPointer(filter.IncludeArchived)
	values[CursorArg] = database.NullStringFromStringPointer(filter.Cursor)
	values[LimitArg] = g.filterLimit(filter.MaxResponseSize)
}

// filterLimit renders the page size bound.
//
// Postgres and SQLite take an expression after LIMIT, so the emitted SQL
// coalesces an absent size to filtering.DefaultQueryFilterLimit and a NULL binds
// correctly. MySQL takes a placeholder and nothing else — COALESCE there is a
// parse error rather than a slower plan — so its LIMIT binds whatever it is
// handed, and what a NULL gets is an empty page.
//
// Binding the constant the other two coalesce to is what keeps that from being a
// dialect a caller has to remember on a path where nothing would remind them: an
// empty page is what a filter that matched nothing looks like. It is filtering's
// constant either way, read from the same place the emitted COALESCE reads it,
// so the two cannot drift.
//
// A size the caller set to zero is left alone, and returns no rows on every
// dialect. That is the documented meaning of an explicit zero — loud, rather
// than a page of some other size — and only absence is defaulted here, the same
// distinction filtering.QueryFilter.Normalize draws.
func (g *Generator) filterLimit(size *uint16) any {
	if size != nil || g.dialect != dialect.MySQL {
		return database.NullInt32FromUint16Pointer(size)
	}

	return int32(filtering.DefaultQueryFilterLimit)
}

// filterTime renders a window bound as the value this dialect's comparisons
// expect.
//
// Postgres and MySQL have a timestamp type and drivers that speak time.Time, so
// a NullTime is what they take. SQLite has neither: its columns hold the text
// CURRENT_TIMESTAMP wrote and its comparisons over one are lexicographic, so a
// time.Time arrives as whatever the driver made of it — and under SQLite's type
// affinity rules a number sorts below every string, so a text column compared
// against one is greater than it whatever the two of them mean.
//
// That is the failure this exists to prevent, and it is the quiet kind: a
// created_after bound as a time on SQLite admits every row in the table, for
// every value of the bound. No error, no empty page, just a window that does
// nothing — which is indistinguishable from a caller who set no window at all.
func (g *Generator) filterTime(at *time.Time) any {
	if g.dialect != dialect.SQLite {
		return database.NullTimeFromTimePointer(at)
	}

	if at == nil {
		return nil
	}

	return at.UTC().Format(SQLiteTimestampLayout)
}
