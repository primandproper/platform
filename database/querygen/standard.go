package querygen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/primandproper/platform-go/v11/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v11/errors"
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
	default:
		return fmt.Sprintf("unknown standard query %d", int(s))
	}
}

// ErrMissingIDColumn indicates a column set without an id. Every query
// StandardCRUD emits keys on it, and the cursor walk orders by it, so there is
// nothing useful to emit for a table that has none.
var ErrMissingIDColumn = platformerrors.New("column set has no id column")

// ErrDuplicateQueryName indicates two emitted queries sharing a name. sqlc turns
// a query name into a Go method name across a whole package, so a duplicate is a
// compile error in generated code, reported against a file nobody wrote.
var ErrDuplicateQueryName = platformerrors.New("two standard queries share a name")

// Option adjusts what StandardCRUD emits.
type Option func(*settings)

type settings struct {
	singular      string
	plural        string
	names         map[StandardQuery]string
	ownership     string
	databaseOwned []string
	immutable     []string
	omitted       []StandardQuery
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
// conventions needs: create, get, exists, list, update, archive, and the id scan
// a search reindex walks.
//
// columns is the table's full column list, in the order the emitted SELECTs
// should list them, and it decides which queries appear. A table without
// archived_at gets no archive; one without last_indexed_at gets no reindex scan;
// one with nothing a caller may assign gets no create and no update. The
// alternative — emitting a query that references a column the table does not
// have — is SQL that fails at sqlc generate for a reason that reads as a schema
// problem.
//
// It panics rather than returning an error, in the manner of regexp.MustCompile.
// Its arguments are string literals in a generator binary, so every way it can
// fail is a typo that a build should stop for, and there is no caller who could
// do anything with an error that the panic does not do more loudly. The panic
// value is an error wrapping dialect.ErrInvalidIdentifier, ErrMissingIDColumn, or
// ErrDuplicateQueryName.
func StandardCRUD(table string, columns []string, opts ...Option) []*Query {
	s := &settings{
		singular: camel(table),
		plural:   camel(table),
		names:    map[StandardQuery]string{},
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

	notUpdatable := append(slices.Clone(s.immutable), s.databaseOwned...)
	if s.ownership != "" {
		notUpdatable = append(notUpdatable, s.ownership)
	}

	insertColumns := ForInsert(columns, s.databaseOwned...)
	updateColumns := ForUpdate(columns, notUpdatable...)

	queries := []*Query{
		s.query(GetQuery, OneType, getStatement(table, columns, s.ownership)),
		s.query(ExistsQuery, OneType, existsStatement(table, columns, s.ownership)),
		s.query(ListQuery, ManyType, listStatement(table, columns, s.ownership)),
	}

	// An INSERT with an empty column list is not a degenerate insert, it is a
	// syntax error. Reaching it takes naming the id itself database-owned.
	if len(insertColumns) > 0 {
		queries = append([]*Query{s.query(CreateQuery, ExecType, createStatement(table, insertColumns))}, queries...)
	}

	if len(updateColumns) > 0 {
		queries = append(queries, s.query(UpdateQuery, ExecRowsType, updateStatement(table, columns, updateColumns, s.ownership)))
	}

	if slices.Contains(columns, ArchivedAtColumn) {
		queries = append(queries, s.query(ArchiveQuery, ExecRowsType, archiveStatement(table, s.ownership)))
	}

	// The scan filters on archived_at, so a table that is indexed but not
	// soft-deletable would get a query naming a column it does not have.
	if slices.Contains(columns, LastIndexedAtColumn) && slices.Contains(columns, ArchivedAtColumn) {
		queries = append(queries, s.query(ScanIDsForReindexQuery, ManyType, ReindexScanQuery(table)))
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
	default:
		return ""
	}
}

func createStatement(table string, insertColumns []string) string {
	values := make([]string, 0, len(insertColumns))
	for _, column := range insertColumns {
		values = append(values, fmt.Sprintf("sqlc.arg(%s)", column))
	}

	return fmt.Sprintf("INSERT INTO %s (\n\t%s\n) VALUES (\n\t%s\n);",
		table,
		strings.Join(insertColumns, ",\n\t"),
		strings.Join(values, ",\n\t"),
	)
}

func getStatement(table string, columns []string, ownership string) string {
	return fmt.Sprintf("SELECT\n\t%s\nFROM %s\nWHERE %s;",
		strings.Join(QualifyAll(table, columns), ",\n\t"),
		table,
		joinPredicates(singleRowPredicates(table, columns, ownership, true), "\t"),
	)
}

func existsStatement(table string, columns []string, ownership string) string {
	return fmt.Sprintf("SELECT EXISTS (\n\tSELECT %s\n\tFROM %s\n\tWHERE %s\n);",
		Qualify(table, IDColumn),
		table,
		joinPredicates(singleRowPredicates(table, columns, ownership, true), "\t\t"),
	)
}

func listStatement(table string, columns []string, ownership string) string {
	var conditions []string
	if ownership != "" {
		conditions = append(conditions, ownershipPredicate(table, ownership, true))
	}

	return fmt.Sprintf("SELECT\n\t%s,\n\t%s,\n\t%s\nFROM %s\nWHERE %s\n%s;",
		strings.Join(QualifyAll(table, columns), ",\n\t"),
		FilterCountSelect(table, columns, nil, conditions...),
		TotalCountSelect(table, columns, nil, conditions...),
		table,
		FilterConditions(table, columns, conditions...),
		CursorLimitClause(table),
	)
}

func updateStatement(table string, columns, updateColumns []string, ownership string) string {
	assignments := make([]string, 0, len(updateColumns)+1)
	for _, column := range updateColumns {
		assignments = append(assignments, fmt.Sprintf("%s = sqlc.arg(%s)", column, column))
	}

	if slices.Contains(columns, LastUpdatedAtColumn) {
		assignments = append(assignments, fmt.Sprintf("%s = %s", LastUpdatedAtColumn, NowExpression))
	}

	return fmt.Sprintf("UPDATE %s SET\n\t%s\nWHERE %s;",
		table,
		strings.Join(assignments, ",\n\t"),
		joinPredicates(singleRowPredicates(table, columns, ownership, false), "\t"),
	)
}

func archiveStatement(table, ownership string) string {
	predicates := []string{
		fmt.Sprintf("%s IS NULL", ArchivedAtColumn),
		fmt.Sprintf("%s = sqlc.arg(%s)", IDColumn, IDColumn),
	}

	if ownership != "" {
		predicates = append(predicates, ownershipPredicate(table, ownership, false))
	}

	return fmt.Sprintf("UPDATE %s SET\n\t%s = %s\nWHERE %s;",
		table,
		ArchivedAtColumn, NowExpression,
		joinPredicates(predicates, "\t"),
	)
}

// singleRowPredicates is the WHERE clause of a query addressing one row by id:
// unarchived, matching id, and owned by the caller where that applies.
//
// It excludes archived rows outright rather than through the include_archived
// toggle. Reading one row by id is not a filtered list, and a caller that wants
// an archived row back wants a different query rather than a flag on this one.
//
// qualified is false for the UPDATE statements, whose SET clause cannot carry a
// table qualifier and whose WHERE therefore does not either.
func singleRowPredicates(table string, columns []string, ownership string, qualified bool) []string {
	name := func(column string) string {
		if qualified {
			return Qualify(table, column)
		}

		return column
	}

	var predicates []string

	if slices.Contains(columns, ArchivedAtColumn) {
		predicates = append(predicates, name(ArchivedAtColumn)+" IS NULL")
	}

	predicates = append(predicates, fmt.Sprintf("%s = sqlc.arg(%s)", name(IDColumn), IDColumn))

	if ownership != "" {
		predicates = append(predicates, ownershipPredicate(table, ownership, qualified))
	}

	return predicates
}

// ownershipPredicate matches the owner column against a bound argument.
func ownershipPredicate(table, column string, qualified bool) string {
	name := column
	if qualified {
		name = Qualify(table, column)
	}

	return fmt.Sprintf("%s = sqlc.arg(%s)", name, column)
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
