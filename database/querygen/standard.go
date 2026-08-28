package querygen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/primandproper/platform-go/v13/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
)

// StandardQuery names one of the queries StandardCRUD emits, for renaming it.
type StandardQuery int

const (
	// CreateQuery inserts a row, taking a value for every column the database
	// does not own.
	CreateQuery StandardQuery = iota
	// GetQuery reads one unarchived row by id.
	GetQuery
	// ExistsQuery reports whether GetQuery would find a row, without reading it.
	ExistsQuery
	// ListQuery reads a filtered, cursor-paginated page along with the two
	// counts filtering.QueryFilteredResult carries.
	ListQuery
	// UpdateQuery assigns every mutable column and stamps last_updated_at.
	UpdateQuery
	// ArchiveQuery soft-deletes a row.
	ArchiveQuery
	// ScanIDsForReindexQuery walks ids in byte order for a search reindex.
	ScanIDsForReindexQuery
	// MarkAsIndexedQuery stamps last_indexed_at on every id it is handed, which
	// is what a search/sync Syncer flushes through once the index has accepted
	// those documents.
	MarkAsIndexedQuery
)

// String names the query, for error messages.
func (s StandardQuery) String() string {
	switch s {
	case CreateQuery:
		return "create"
	case GetQuery:
		return "get"
	case ExistsQuery:
		return "exists"
	case ListQuery:
		return "list"
	case UpdateQuery:
		return "update"
	case ArchiveQuery:
		return "archive"
	case ScanIDsForReindexQuery:
		return "scan IDs for reindex"
	case MarkAsIndexedQuery:
		return "mark as indexed"
	default:
		return fmt.Sprintf("unknown standard query %d", int(s))
	}
}

// ErrMissingIDColumn indicates a column set without an id, handed to
// StandardCRUD.
//
// It is StandardCRUD's requirement rather than the package's, and the two halves
// differ on purpose. StandardCRUD emits the list query, and the list pages by
// keyset over the id — CursorCondition compares against it and IDColumn's own
// comment records that the column has to sort by creation time for that walk to
// page in a sensible order. A composite key is not a cursor, so there is nothing
// useful to emit for a table that has none.
//
// The Bound methods carry no such requirement. A store knows what it keys on,
// and says so with Match values; a table whose primary key is (subject_type,
// subject_id) passes two of them and addresses a row exactly. What it does not
// get is a paged list — see the package comment.
var ErrMissingIDColumn = platformerrors.New("column set has no id column")

// ErrUnaddressableRow indicates a single-row statement with nothing to key on:
// no id column, no ownership column, and no Match. The statement it would
// otherwise render is one whose WHERE clause is the archived predicate and
// nothing else, which reads one row from a table by reading all of them, and
// updates or archives every row in it.
//
// It is a programming error rather than a caller's — nothing on a request path
// decides which columns a statement keys on — so it panics like the rest of this
// package's misuse.
//
// A guard counts as something said about which rows, and that is deliberate
// rather than an oversight in the check. A sweep — every live row past its
// deadline, archived in one statement — keys on the clock and on nothing a
// caller binds, and it is a statement somebody means: expiring sessions is not a
// single-row write that forgot its key. So a lone Match{Against: CurrentTime}
// renders, and what the check still refuses is a statement that says nothing at
// all about which rows it is for.
//
// The corollary is worth stating, because it is the one way a guard can widen
// rather than narrow. A lone Match{Column: ArchivedAtColumn, Against: NoValue}
// says exactly what the archived predicate already said, so the statement it
// renders is the whole-table write this error is named for, and nothing here
// will stop it. A guard is a narrowing only when it narrows.
var ErrUnaddressableRow = platformerrors.New("single-row statement keys on nothing")

// ErrArgumentlessMatch indicates a Match naming an argument its comparand has
// nowhere to put: an Arg beside IS NULL, beside the empty-string guard, or
// beside a clock comparison, none of which bind anything.
//
// It is a panic rather than a silently ignored field because the two readings a
// caller could have had are both wrong in a way nothing downstream would report.
// A caller who meant `column = sqlc.arg(other)` and reached for NoValue gets a
// statement with one fewer argument than they are about to bind, which sqlc
// turns into a params struct missing a field; a caller who meant the guard and
// named an argument out of habit has written a name that no marker will ever
// carry, which is a name their argument map can hold forever without anything
// noticing.
var ErrArgumentlessMatch = platformerrors.New("match names an argument its comparand cannot bind")

// ErrDuplicateQueryName indicates two emitted queries sharing a name. sqlc turns
// a query name into a Go method name across a whole package, so a duplicate is a
// compile error in generated code, reported against a file nobody wrote.
var ErrDuplicateQueryName = platformerrors.New("two standard queries share a name")

// Option adjusts what StandardCRUD emits.
type Option func(*settings)

type settings struct {
	registry      *Registry
	names         map[StandardQuery]string
	singular      string
	plural        string
	ownership     string
	databaseOwned []string
	immutable     []string
	omitted       []StandardQuery
	nullable      []string
}

// WithEntity sets the singular and plural entity names the default query names
// are built from — WithEntity("valid instrument", "valid instruments") is
// written as WithEntity("ValidInstrument", "ValidInstruments").
//
// Both default to the table name in upper camel case, which makes the default
// names correct but plural throughout: GetValidInstruments reads one row. The
// singular is not derived from the table, because deriving it means guessing
// whether the table is statuses, indices, or data, and a generator that guesses
// its callers' method names is a generator whose output has to be read to be
// trusted.
func WithEntity(singular, plural string) Option {
	return func(s *settings) {
		s.singular = singular
		s.plural = plural
	}
}

// WithQueryName renames one query, for a consumer whose existing generated code
// spells it differently.
func WithQueryName(query StandardQuery, name string) Option {
	return func(s *settings) {
		s.names[query] = name
	}
}

// WithOmitted drops queries from the set, for a table whose rows are not
// addressable the way the whole set assumes.
//
// Not every table following these conventions is a resource. A child row written
// as part of its parent and only ever read through it has no caller for a get by
// id, an exists, or a list, and emitting them anyway produces generated methods
// nobody calls next to a read path that answers without whatever scoping the
// parent's own queries apply — the sort of query that is found later by someone
// looking for a convenient way to fetch a row.
//
// It only subtracts. What StandardCRUD emits stays a subset of what the column
// list justifies, so a table without archived_at still cannot acquire an Archive
// and this option cannot conjure a query the columns do not support. Naming a
// query the columns already exclude is not an error; it says the same thing twice.
//
// Omitting everything yields an empty slice, which RenderFile renders as the
// empty string rather than a file with no queries in it.
func WithOmitted(queries ...StandardQuery) Option {
	return func(s *settings) {
		s.omitted = append(s.omitted, queries...)
	}
}

// WithNullable names columns an INSERT or an UPDATE may set to NULL, binding
// them with sqlc.narg rather than sqlc.arg so the generated Go parameter is a
// pointer instead of a value.
//
// It cannot be derived. A column list is names, and whether the column behind one
// is NOT NULL lives in the schema this package never reads. Nor does getting it
// wrong stop a build: sqlc generates against the schema, so an omitted nullable
// column yields a parameter that cannot express the NULL the column accepts, and
// a column named here that is NOT NULL yields one that can express a NULL the
// database will reject at runtime. Both are quiet, which is why they are declared
// at the table rather than inferred from one.
//
// Reads are unaffected — a SELECT lists the column either way.
func WithNullable(columns ...string) Option {
	return func(s *settings) {
		s.nullable = append(s.nullable, columns...)
	}
}

// WithOwnership scopes the single-row queries and the list to an owner column —
// BelongsToAccountColumn, conventionally — so that every one of them takes the
// owner as an argument and a row belonging to someone else is not found rather
// than found and returned.
//
// It is opt-in rather than inferred from the column set. Inferring it would mean
// that renaming a column, or building a table's generator from a column list that
// happens to omit one, silently widens who can read every row — the class of
// change that looks like nothing in a diff.
//
// The column is also excluded from UPDATE, since a row that can reassign its own
// owner makes the scope on every other query a formality.
func WithOwnership(column string) Option {
	return func(s *settings) {
		s.ownership = column
	}
}

// WithDatabaseOwned names further columns the database fills in, beyond the four
// this package already knows about, excluding them from both INSERT and UPDATE.
func WithDatabaseOwned(columns ...string) Option {
	return func(s *settings) {
		s.databaseOwned = append(s.databaseOwned, columns...)
	}
}

// WithImmutable names columns that are set once at insert and never assigned
// again — the row's creator, the parent it hangs off — excluding them from
// UPDATE only.
func WithImmutable(columns ...string) Option {
	return func(s *settings) {
		s.immutable = append(s.immutable, columns...)
	}
}

// StandardCRUD emits the queries every table following this module's row
// conventions needs: create, get, exists, list, update, archive, the id scan a
// search reindex walks, and the stamp that maintains the column the scan reads.
//
// columns is the table's full column list, in the order the emitted SELECTs
// should list them, and it decides which queries appear. A table without
// archived_at gets no archive; one without last_indexed_at gets neither the
// reindex scan nor the stamp; one with nothing a caller may assign gets no
// create and no update. The alternative — emitting a query that references a
// column the table does not have — is SQL that fails at sqlc generate for a
// reason that reads as a schema problem.
//
// It panics rather than returning an error, in the manner of regexp.MustCompile.
// Its arguments are string literals in a generator binary, so every way it can
// fail is a typo that a build should stop for, and there is no caller who could
// do anything with an error that the panic does not do more loudly. The panic
// value is an error wrapping dialect.ErrInvalidIdentifier, ErrMissingIDColumn, or
// ErrDuplicateQueryName.
//
// It also registers the table — see [Registry]. That is the half of this call a
// consumer needs when it stops making it: a table's queries can move somewhere
// else, but the table still exists and still has rows in it, and the list a
// consumer reads back should not shorten because something else started
// producing the SQL. [WithRegistry] chooses where the name lands.
//
// Which queries appear does not depend on the dialect, and neither do their
// names. A table generated for Postgres and the same table generated for SQLite
// yield the same set of sqlc methods with the same signatures — bar the two
// places sqlc's own type inference differs, which the package comment names — so
// the application code above them is written once. What differs is the SQL under
// each name.
func (g *Generator) StandardCRUD(table string, columns []string, opts ...Option) []*Query {
	s := &settings{
		singular: camel(table),
		plural:   camel(table),
		names:    map[StandardQuery]string{},
		registry: defaultRegistry,
	}

	for _, opt := range opts {
		opt(s)
	}

	// The names are interpolated into statement text rather than bound, so they
	// are restricted rather than escaped — see dialect.ValidIdentifier.
	mustIdentifier("table name", table)

	for _, column := range columns {
		mustIdentifier("column name", column)
	}

	if s.ownership != "" {
		mustIdentifier("ownership column", s.ownership)
	}

	if !slices.Contains(columns, IDColumn) {
		panic(platformerrors.Wrapf(ErrMissingIDColumn, "querygen: table %q", table))
	}

	// The table is registered for existing, not for the queries below. A table
	// whose whole set WithOmitted removes still has rows in it, and the list
	// this feeds is read by whoever has to truncate them — see Registry.
	s.registry.Register(table)

	notUpdatable := append(slices.Clone(s.immutable), s.databaseOwned...)
	if s.ownership != "" {
		notUpdatable = append(notUpdatable, s.ownership)
	}

	insertColumns := ForInsert(columns, s.databaseOwned...)
	updateColumns := ForUpdate(columns, notUpdatable...)

	queries := []*Query{
		s.query(GetQuery, OneType, g.getStatement(table, columns, s.ownership, Read{})),
		s.query(ExistsQuery, OneType, g.existsStatement(table, columns, s.ownership)),
		s.query(ListQuery, ManyType, g.listStatement(table, columns, s.ownership, nil)),
	}

	// An INSERT with an empty column list is not a degenerate insert, it is a
	// syntax error. Reaching it takes naming the id itself database-owned.
	if len(insertColumns) > 0 {
		queries = append([]*Query{s.query(CreateQuery, ExecType, createStatement(table, insertColumns, s.nullable))}, queries...)
	}

	if len(updateColumns) > 0 {
		queries = append(queries, s.query(UpdateQuery, ExecRowsType, g.updateStatement(table, columns, updateColumns, s.ownership, s.nullable)))
	}

	if slices.Contains(columns, ArchivedAtColumn) {
		queries = append(queries, s.query(ArchiveQuery, ExecRowsType, g.archiveStatement(table, columns, s.ownership)))
	}

	if slices.Contains(columns, LastIndexedAtColumn) {
		// The scan filters on archived_at, so a table that is indexed but not
		// soft-deletable would get a query naming a column it does not have.
		// The stamp names no column but the two it assigns and keys on, so it
		// is emitted for every indexed table — which is what keeps the column
		// from being one nothing can write.
		if slices.Contains(columns, ArchivedAtColumn) {
			queries = append(queries, s.query(ScanIDsForReindexQuery, ManyType, g.ReindexScanQuery(table)))
		}

		queries = append(queries, s.query(MarkAsIndexedQuery, ExecRowsType, g.IndexStampQuery(table)))
	}

	queries = slices.DeleteFunc(queries, func(query *Query) bool { return query == nil })

	mustBeUniquelyNamed(table, queries)

	return queries
}

// query builds one annotated query under its configured or default name, or nil
// when WithOmitted named it. StandardCRUD drops the nils, which keeps the
// decision in one place rather than at each of the seven call sites.
func (s *settings) query(which StandardQuery, queryType QueryType, content string) *Query {
	if slices.Contains(s.omitted, which) {
		return nil
	}

	return &Query{
		Annotation: QueryAnnotation{Name: s.name(which), Type: queryType},
		Content:    content,
	}
}

// name returns the configured name for which, or the default built from the
// entity names.
func (s *settings) name(which StandardQuery) string {
	if name, ok := s.names[which]; ok {
		return name
	}

	switch which {
	case CreateQuery:
		return "Create" + s.singular
	case GetQuery:
		return "Get" + s.singular
	case ExistsQuery:
		return "Check" + s.singular + "Existence"
	case ListQuery:
		// List rather than Get, so that the default entity names — where the
		// singular and the plural are both the table name — cannot collide with
		// the single-row read.
		return "List" + s.plural
	case UpdateQuery:
		return "Update" + s.singular
	case ArchiveQuery:
		return "Archive" + s.singular
	case ScanIDsForReindexQuery:
		return "Scan" + s.singular + "IDsForReindex"
	case MarkAsIndexedQuery:
		return "Mark" + s.plural + "AsIndexed"
	default:
		return ""
	}
}

// binding renders the sqlc argument a write binds column through: narg for the
// columns WithNullable named, arg for the rest.
func binding(column string, nullable []string) string {
	if slices.Contains(nullable, column) {
		return fmt.Sprintf("sqlc.narg(%s)", column)
	}

	return fmt.Sprintf("sqlc.arg(%s)", column)
}

func createStatement(table string, insertColumns, nullable []string) string {
	values := make([]string, 0, len(insertColumns))
	for _, column := range insertColumns {
		values = append(values, binding(column, nullable))
	}

	return fmt.Sprintf("INSERT INTO %s (\n\t%s\n) VALUES (\n\t%s\n);",
		table,
		strings.Join(insertColumns, ",\n\t"),
		strings.Join(values, ",\n\t"),
	)
}

// getStatement renders the read of one row: what read projects, from table,
// keyed on whatever the column list and the matches say addresses a row.
//
// columns is the table's shape rather than the projection — it is what the id
// and archived predicates are derived from — and read.Projection is what the
// SELECT lists. The two are the same list for the standard get and differ for
// every keyed read that returns one column, or that projects an id it does not
// key on.
func (g *Generator) getStatement(table string, columns []string, ownership string, read Read, extra ...Match) string {
	return fmt.Sprintf("SELECT\n\t%s\nFROM %s\nWHERE %s%s;",
		strings.Join(QualifyAll(table, read.projecting(columns)), ",\n\t"),
		table,
		joinPredicates(g.singleRowPredicates(table, columns, ownership, true, extra...), "\t"),
		orderClause(table, read.Order),
	)
}

// orderClause renders the ordering a keyed read whose key admits more than one
// row picks with, and nothing at all for a key that identifies a row.
//
// The page size is the literal 1 rather than a bound argument, because it is
// the statement's own shape rather than a caller's choice: this is a :one read,
// and a caller wanting a page of these wants the list query.
func orderClause(table, column string) string {
	if column == "" {
		return ""
	}

	return fmt.Sprintf("\nORDER BY %s ASC\nLIMIT 1", Qualify(table, column))
}

func (g *Generator) existsStatement(table string, columns []string, ownership string, extra ...Match) string {
	return fmt.Sprintf("SELECT EXISTS (\n\tSELECT %s\n\tFROM %s\n\tWHERE %s\n);",
		existsProjection(table, columns),
		table,
		joinPredicates(g.singleRowPredicates(table, columns, ownership, true, extra...), "\t\t"),
	)
}

// existsProjection is what the EXISTS subquery selects. Nothing reads it — EXISTS
// answers from whether a row was produced, not from what was in it — so a table
// with no id selects the literal every server accepts there, rather than a column
// this package would have to pick for it.
//
// The id case keeps selecting the id because it always has, and an existence
// check whose text changed for every conventional table in the module would be a
// diff nobody could read for the one line in it that mattered.
func existsProjection(table string, columns []string) string {
	if slices.Contains(columns, IDColumn) {
		return Qualify(table, IDColumn)
	}

	return "1"
}

// listStatement is the one filtered read this package emits. It takes a
// junction because the joined form is this statement with a join spliced into
// its FROM rather than a statement of its own — see junction.go for why that is
// worth a parameter every single-table caller passes a zero value for.
func (g *Generator) listStatement(table string, columns []string, ownership string, junction *Junction, extra ...Match) string {
	var conditions []string
	if ownership != "" {
		conditions = append(conditions, g.equalityPredicate(table, ownership, true))
	}

	conditions = append(conditions, g.matchPredicates(table, true, extra)...)
	conditions = append(conditions, junction.conditions(g)...)

	joins := junction.joins(table)

	return fmt.Sprintf("SELECT\n\t%s,\n\t%s,\n\t%s\n%s\nWHERE %s\n%s;",
		strings.Join(append(QualifyAll(table, columns), junction.projection()...), ",\n\t"),
		g.FilterCountSelect(table, columns, joins, conditions...),
		g.TotalCountSelect(table, columns, joins, conditions...),
		fromClause(table, joins),
		g.FilterConditions(table, columns, conditions...),
		g.CursorLimitClause(table),
	)
}

func (g *Generator) updateStatement(table string, columns, updateColumns []string, ownership string, nullable []string, extra ...Match) string {
	assignments := make([]string, 0, len(updateColumns)+1)
	for _, column := range updateColumns {
		assignments = append(assignments, fmt.Sprintf("%s = %s", column, binding(column, nullable)))
	}

	if slices.Contains(columns, LastUpdatedAtColumn) {
		assignments = append(assignments, fmt.Sprintf("%s = %s", LastUpdatedAtColumn, g.storedNow()))
	}

	return fmt.Sprintf("UPDATE %s SET\n\t%s\nWHERE %s;",
		table,
		strings.Join(assignments, ",\n\t"),
		joinPredicates(g.singleRowPredicates(table, columns, ownership, false, extra...), "\t"),
	)
}

// archiveStatement renders the soft delete. It takes the column list for the
// same reason every other statement here does — the predicates it keys on are
// derived from it — and routes through singleRowPredicates rather than building
// its own list, so there is one rendering of "this row, unarchived, and mine"
// rather than two that could come to disagree about it.
func (g *Generator) archiveStatement(table string, columns []string, ownership string, extra ...Match) string {
	return fmt.Sprintf("UPDATE %s SET\n\t%s = %s\nWHERE %s;",
		table,
		ArchivedAtColumn, g.storedNow(),
		joinPredicates(g.singleRowPredicates(table, columns, ownership, false, extra...), "\t"),
	)
}

// singleRowPredicates is the WHERE clause of a query addressing one row:
// unarchived, matching whatever keys the row, and owned by the caller where that
// applies.
//
// The id predicate is conditional on the column being present, exactly as the
// archived one two lines above it is, and for the same reason: this package's
// whole design is that a column the table does not have produces no predicate.
// A handful of tables in this module have none — their primary key is a natural
// key that carries meaning, and (subject_type, subject_id) is what enforces one
// live key per subject rather than a surrogate standing in for it. Such a table
// names its key in Match values instead, which is what Match is already for.
//
// What it will not do is render nothing. A statement with no id, no ownership
// column and no matches keys on the archived predicate alone, which addresses
// every live row in the table rather than one — so that is ErrUnaddressableRow
// rather than a statement.
//
// It excludes archived rows outright rather than through the include_archived
// toggle. Reading one row by its key is not a filtered list, and a caller that
// wants an archived row back wants a different query rather than a flag on this
// one.
//
// qualified is false for the UPDATE statements, whose SET clause cannot carry a
// table qualifier and whose WHERE therefore does not either.
func (g *Generator) singleRowPredicates(table string, columns []string, ownership string, qualified bool, extra ...Match) []string {
	keyed := slices.Contains(columns, IDColumn) || ownership != "" || len(extra) > 0
	if !keyed {
		panic(platformerrors.Wrapf(ErrUnaddressableRow, "querygen: table %q", table))
	}

	var predicates []string

	if slices.Contains(columns, ArchivedAtColumn) {
		name := ArchivedAtColumn
		if qualified {
			name = Qualify(table, ArchivedAtColumn)
		}

		predicates = append(predicates, name+" IS NULL")
	}

	if slices.Contains(columns, IDColumn) {
		predicates = append(predicates, g.equalityPredicate(table, IDColumn, qualified))
	}

	if ownership != "" {
		predicates = append(predicates, g.equalityPredicate(table, ownership, qualified))
	}

	predicates = append(predicates, g.matchPredicates(table, qualified, extra)...)

	return predicates
}

// equalityPredicate matches a column against a bound argument. It is the one
// place a keyed predicate is rendered, whether the key is the owner column
// WithOwnership names or one of the Match columns a bound statement adds — the
// two say the same thing about a row and there is no version of this that is
// right for one and wrong for the other.
func (g *Generator) equalityPredicate(table, column string, qualified bool) string {
	return g.matchPredicate(table, Match{Column: column}, qualified)
}

// matchPredicate renders one match: the column against whatever its comparand
// says, in the operator Exclude chooses.
//
// The excluded form of a bound match binds the same argument under the same name
// as the included one, so a caller assembling an argument map keys on the column
// either way and nothing downstream has to know which operator the statement
// carries. The argument name is the column's unless the match names another —
// see Match.Arg, which is what a guard naming a column the same statement
// assigns needs.
//
// The guard comparands render no argument at all, which is what makes them
// guards: the value the predicate compares against belongs to the statement, so
// there is nothing a caller could pass that would relax it. Exclude complements
// each of them rather than switching between unrelated questions — see
// Match.Exclude — so there is one rendering of the boundary per comparand and
// not one per direction.
//
// It hangs off Generator for the clock, which is the one comparand the three
// dialects do not spell identically. Routing it through storedNow keeps that
// divergence on generator.go's one screen, and keeps a statement's comparisons
// asking for the time in the units its assignments write it in.
func (g *Generator) matchPredicate(table string, match Match, qualified bool) string {
	if match.Arg != "" && !match.Against.binds() {
		panic(platformerrors.Wrapf(ErrArgumentlessMatch,
			"querygen: column %q is compared against %s and names argument %q",
			match.Column, match.Against, match.Arg))
	}

	name := match.Column
	if qualified {
		name = Qualify(table, match.Column)
	}

	switch match.Against {
	case NoValue:
		if match.Exclude {
			return name + " IS NOT NULL"
		}

		return name + " IS NULL"
	case EmptyString:
		return fmt.Sprintf("%s %s ''", name, match.operator())
	case CurrentTime:
		// The complement of "at or before now" is "strictly after now", so the
		// two forms partition the rows rather than overlapping at the instant a
		// deadline falls on.
		operator := "<="
		if match.Exclude {
			operator = ">"
		}

		return fmt.Sprintf("%s %s %s", name, operator, g.storedNow())
	case OptionalArgument:
		return fmt.Sprintf("%s %s COALESCE(sqlc.narg(%s), '')", name, match.operator(), match.argument())
	// BoundArgument, which is the zero value and every keyed read's comparand.
	default:
		return fmt.Sprintf("%s %s sqlc.arg(%s)", name, match.operator(), match.argument())
	}
}

// matchPredicates renders one predicate per match.
//
// The matches are the dimensions a caller adds beyond the row's own id — a
// tenancy scope column, the owner the sqlc path expresses through
// WithOwnership, the guards a write puts its correctness on. They all go through
// matchPredicate, so a keyed read and a guarded write filter a row the same way,
// and a caller can add a tenancy scope column without this package knowing what
// a tenancy scope is.
func (g *Generator) matchPredicates(table string, qualified bool, matches []Match) []string {
	predicates := make([]string, 0, len(matches))
	for i := range matches {
		predicates = append(predicates, g.matchPredicate(table, matches[i], qualified))
	}

	return predicates
}

// mustBeUniquelyNamed panics when two of the emitted queries share a name, which
// WithQueryName makes reachable.
func mustBeUniquelyNamed(table string, queries []*Query) {
	seen := make(map[string]struct{}, len(queries))

	for _, query := range queries {
		if _, ok := seen[query.Annotation.Name]; ok {
			panic(platformerrors.Wrapf(ErrDuplicateQueryName, "querygen: table %q query %q", table, query.Annotation.Name))
		}

		seen[query.Annotation.Name] = struct{}{}
	}
}

// mustIdentifier panics unless name is safe to interpolate into statement text.
func mustIdentifier(kind, name string) {
	if !dialect.ValidIdentifier(name) {
		panic(platformerrors.Wrapf(dialect.ErrInvalidIdentifier, "querygen: %s %q", kind, name))
	}
}

// camel renders a snake_case table name in upper camel case, which is the shape
// a sqlc query name — and so a generated Go method name — takes.
func camel(table string) string {
	words := strings.Split(table, "_")

	out := make([]string, 0, len(words))
	for _, word := range words {
		if word == "" {
			continue
		}

		out = append(out, strings.ToUpper(word[:1])+word[1:])
	}

	return strings.Join(out, "")
}
