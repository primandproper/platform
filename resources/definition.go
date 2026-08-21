package resources

import (
	"context"
	"slices"
	"strings"

	"github.com/primandproper/platform-go/v12/database/dialect"
	"github.com/primandproper/platform-go/v12/database/querygen"
	platformerrors "github.com/primandproper/platform-go/v12/errors"
)

var (
	// ErrNoIDColumn indicates a definition with no ID column. Every statement
	// keys on it and the cursor walk orders by it, so there is nothing useful to
	// build for a resource that has none.
	ErrNoIDColumn = platformerrors.New("definition has no id column")

	// ErrDuplicateColumn indicates a column name declared twice.
	ErrDuplicateColumn = platformerrors.New("column declared more than once")

	// ErrConflictingRole indicates a second id, scope, or owner column, or a
	// scope column on a definition that also declared Unscoped.
	ErrConflictingRole = platformerrors.New("conflicting column roles")

	// ErrUnknownColumn indicates a lookup or match naming a column the
	// definition does not have.
	ErrUnknownColumn = platformerrors.New("no such column")

	// ErrUndeclaredLookup indicates a List or ArchiveMatching whose match set
	// does not correspond to any declared Lookup.
	//
	// It is a programming error rather than a caller's. A generic list method
	// that accepted any predicate combination would answer queries nobody chose
	// to index, and the first anyone hears of it is a table scan in production —
	// so the combinations are declared, and one that was not is refused here
	// rather than at the database.
	ErrUndeclaredLookup = platformerrors.New("no lookup declared for this match set")

	// ErrMatchTypeMismatch indicates a Match whose value is not a plausible
	// value for the column it names — see Column.accepts.
	ErrMatchTypeMismatch = platformerrors.New("match value does not fit the column")

	// ErrLookupOnPredicateColumn indicates a Lookup naming a column the store
	// already binds a predicate on: the tenancy scope, or an owner that gates
	// reads.
	//
	// A match set is bound into the same argument map the scope and the owner
	// were bound into, keyed by column name, so a lookup on one of those columns
	// binds the caller's value where the gate's value was. The statement still
	// carries the predicate and still looks correct; it simply compares against
	// whatever the read asked for. That is a tenant reading another tenant's
	// rows by naming them, and an actor reading another actor's private rows by
	// naming them.
	//
	// It is refused where the lookup is declared rather than where the match
	// arrives, because a declaration is the only way a match set reaches a
	// statement — listFor and archiveMatchingFor both refuse a set no Lookup
	// declared. Catching it at construction means the resource that could have
	// leaked never exists, rather than existing until someone calls it the wrong
	// way.
	//
	// An owner that gates only writes is not affected: its reads carry no owner
	// predicate, so On("belongs_to_user") is the ordinary way to ask for one
	// author's rows — which is what a comment's reference read is.
	ErrLookupOnPredicateColumn = platformerrors.New("lookup names a column the store already binds a predicate on")

	// ErrScopeNotSupported indicates a scope naming a tenant on a resource
	// declared Unscoped, whose table has no column to filter it by.
	ErrScopeNotSupported = platformerrors.New("resource is unscoped and cannot answer for a tenant")
)

// Scoping says how a resource's rows carry their tenancy dimension.
type Scoping uint8

const (
	// ScopedByColumn is the default: the definition includes a Scope column, and
	// every read filters by it.
	ScopedByColumn Scoping = iota
	// Unscoped means the table has no scope column and every row is
	// tenancy.Global.
	//
	// Its methods still take a tenancy.Scope and still refuse one that names a
	// tenant, so the call sites are the ones a scoped resource has. Adding the
	// column later is a change to this declaration and to the migration, and to
	// nothing else.
	Unscoped
)

// Lookup is a combination of columns a List or ArchiveMatching may key on.
//
// It exists so that the set of queries this package can issue is a set someone
// chose. Declaring one is the moment to ask whether the index behind it exists —
// which is a question a store with a generic list method never makes anyone ask.
//
// It may not name a column the store binds a predicate on for itself — the
// tenancy scope, or an owner that gates reads. See ErrLookupOnPredicateColumn.
type Lookup struct {
	columns []string
}

// On declares a lookup over the given columns. Order does not matter: a match
// set is compared against it as a set.
func On(columns ...string) Lookup {
	return Lookup{columns: columns}
}

// key renders a lookup's columns as an order-independent identity.
func (l Lookup) key() string {
	sorted := slices.Clone(l.columns)
	slices.Sort(sorted)

	return strings.Join(sorted, ",")
}

// Definition declares one resource: its table, its columns, and the roles picked
// out of them.
//
// It is a plain struct rather than a builder because it is written once per
// resource, as a literal, and read far more often than it is written. Define
// turns it into the Resource a Store is built from, and is where everything that
// can be checked about it is checked.
type Definition[T any] struct {
	// Validate is run against a row before it is written, if set. It is the seam
	// for whatever an application validates with — this package has no opinion,
	// and deliberately does not reach for a validation library of its own.
	Validate func(context.Context, *T) error

	// Registry is where the table registers its existence, and defaults to
	// querygen's own. See Define.
	Registry *querygen.Registry

	// Name is the resource in the singular, in lower snake case: "comment",
	// "waitlist_signup". It names spans, log fields, metric attributes, and the
	// resource type a hook reports.
	Name string

	// Table is the table name. It is interpolated into statement text, not
	// bound, and is therefore restricted rather than escaped.
	Table string

	// Columns is the table's full column list, in the order reads should list
	// them. Which of the conventional columns it contains decides what the
	// resource can do — no archived_at means no Archive.
	Columns []Column[T]

	// Lookups are the column combinations List and ArchiveMatching may key on,
	// beyond a read of everything in scope.
	Lookups []Lookup

	// Scoping says whether the resource carries a tenancy scope column. The zero
	// value expects one among Columns.
	Scoping Scoping
}

// Resource is a checked Definition: the statements it needs, rendered, and the
// roles it declared, resolved.
//
// It is built once, at construction, and holds no database handle — a Store
// pairs it with one. Rendering the statements here rather than per call is what
// makes a Store's methods a bind and an execute.
type Resource[T any] struct {
	archiveMatchingByLookup map[string]querygen.Bound

	columnsByName map[string]Column[T]
	owner         *Column[T]
	scope         *Column[T]

	generator *querygen.Generator

	id Column[T]

	// The statements, rendered once, in the two variants a call chooses between
	// by who is making it. See statements.
	asOwner, asSystem statements

	// create is the one statement that is the same either way: an insert names
	// no predicate for an owner to appear in.
	create querygen.Bound

	columns       []Column[T]
	columnNames   []string
	insertColumns []string
	updateColumns []string

	def Definition[T]

	softDeletes bool
}

// statements is one rendering of everything a resource can issue, for one kind
// of actor.
//
// There are two of them because the owner is a predicate, and a predicate is
// either in a statement or it is not. A user's call is keyed on the owner
// wherever the declaration says the owner gates that operation; the system
// actor's call is not keyed on it at all, which is what lets a retention reaper
// list every author's private notes and a cascade archive every author's
// comments.
//
// The alternative — one rendering, with the owner bound to something harmless
// for the system actor — has no harmless value to bind. An empty string matches
// no row and would turn "the platform reads everything" into "the platform
// reads nothing", silently, on the path nobody watches.
type statements struct {
	listsByLookup map[string]querygen.Bound

	get, exists, list querygen.Bound
	update, archive   querygen.Bound

	// matches are the predicate columns this variant's reads carry beyond the
	// id, so a statement rendered per call — the set read — is rendered with
	// the same ones.
	matches []querygen.Match
}

// Name returns the resource's name.
func (r *Resource[T]) Name() string { return r.def.Name }

// Table returns the resource's table name.
func (r *Resource[T]) Table() string { return r.def.Table }

// Define checks a definition and renders the statements it implies.
//
// Everything it can refuse, it refuses here rather than at the first call: a
// duplicate column, an unknown column in a lookup, a missing id, a scope column
// on a resource declaring itself unscoped. A resource is declared once at
// startup and used for the process's lifetime, so a declaration error found at
// construction is found before any traffic and a declaration error found at call
// time is found by a user.
//
// It also registers the table, in querygen's registry or in the one the
// definition named. A table served here emits no generated queries, and a
// consumer that derived its table list from what its generator emitted would
// therefore lose it — which shows up as a maintenance TRUNCATE that skips one
// table and a test failing somewhere else later. Registering the name is what
// survives the queries going away; see querygen.Registry.
func Define[T any](d dialect.Dialect, def Definition[T]) (*Resource[T], error) {
	if !d.Valid() {
		return nil, platformerrors.Wrapf(dialect.ErrUnsupported, "resources: dialect %q", d)
	}

	if def.Name == "" || def.Table == "" {
		return nil, platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "resources: a definition needs a name and a table")
	}

	if !dialect.ValidIdentifier(def.Table) {
		return nil, platformerrors.Wrapf(dialect.ErrInvalidIdentifier, "resources: table %q", def.Table)
	}

	r := &Resource[T]{
		def:                     def,
		columns:                 def.Columns,
		columnsByName:           make(map[string]Column[T], len(def.Columns)),
		generator:               querygen.For(d),
		archiveMatchingByLookup: map[string]querygen.Bound{},
	}

	for i := range def.Columns {
		column := def.Columns[i]

		if !dialect.ValidIdentifier(column.name) {
			return nil, platformerrors.Wrapf(dialect.ErrInvalidIdentifier, "resources: column %q", column.name)
		}

		if _, duplicate := r.columnsByName[column.name]; duplicate {
			return nil, platformerrors.Wrapf(ErrDuplicateColumn, "resources: column %q", column.name)
		}

		r.columnsByName[column.name] = column
		r.columnNames = append(r.columnNames, column.name)

		switch column.role {
		case roleID:
			if r.id.ref != nil {
				return nil, platformerrors.Wrap(ErrConflictingRole, "resources: two id columns")
			}

			r.id = column
		case roleOwner:
			if r.owner != nil {
				return nil, platformerrors.Wrap(ErrConflictingRole, "resources: two owner columns")
			}

			r.owner = &column
		case roleScope:
			if r.scope != nil {
				return nil, platformerrors.Wrap(ErrConflictingRole, "resources: two scope columns")
			}

			if def.Scoping == Unscoped {
				return nil, platformerrors.Wrap(ErrConflictingRole, "resources: an unscoped resource declared a scope column")
			}

			r.scope = &column
		case roleData:
		}
	}

	if r.id.ref == nil {
		return nil, platformerrors.Wrapf(ErrNoIDColumn, "resources: table %q", def.Table)
	}

	if def.Scoping == ScopedByColumn && r.scope == nil {
		return nil, platformerrors.Wrap(ErrConflictingRole, "resources: a scoped resource declared no scope column — say Unscoped if the table has no such column")
	}

	r.softDeletes = slices.Contains(r.columnNames, querygen.ArchivedAtColumn)

	if err := r.render(); err != nil {
		return nil, err
	}

	r.register()

	return r, nil
}

// MustDefine is Define for a declaration written as a literal, panicking rather
// than returning an error, in the manner of regexp.MustCompile.
//
// Every way a definition can fail is a mistake in a literal that a package
// variable's initialization should stop for — a misspelled column, a lookup over
// a column that does not exist. A caller assembling a definition from
// configuration should call Define and report the rejection in its own terms.
func MustDefine[T any](d dialect.Dialect, def Definition[T]) *Resource[T] {
	resource, err := Define(d, def)
	if err != nil {
		panic(err)
	}

	return resource
}

// register adds the table to the registry the definition named, or to querygen's
// own.
//
// It happens after the statements render, so a declaration that is refused
// registers nothing: a name in the list is a table something is prepared to
// serve, not one somebody tried to declare.
func (r *Resource[T]) register() {
	if r.def.Registry != nil {
		r.def.Registry.Register(r.def.Table)

		return
	}

	querygen.RegisterTable(r.def.Table)
}

// render builds every statement the resource can issue.
func (r *Resource[T]) render() error {
	var immutable []string

	for _, column := range r.columns {
		if column.immutable {
			immutable = append(immutable, column.name)
		}
	}

	// The owner and the scope are never reassigned by an update. An update that
	// could move a row to another tenant is not an update.
	if r.owner != nil {
		immutable = append(immutable, r.owner.name)
	}

	if r.scope != nil {
		immutable = append(immutable, r.scope.name)
	}

	r.insertColumns = querygen.ForInsert(r.columnNames)
	r.updateColumns = querygen.ForUpdate(r.columnNames, immutable...)

	var nullable []string

	for _, column := range r.columns {
		if column.nullable {
			nullable = append(nullable, column.name)
		}
	}

	r.create = r.generator.BoundCreate(r.def.Table, r.insertColumns, nullable)

	// The system actor's statements are keyed on the scope and nothing else.
	// A user's add the owner: to every statement where the owner gates reads,
	// and to the writes wherever there is an owner at all.
	systemMatches := r.scopeMatches()

	readMatches := systemMatches
	if r.owner != nil && r.owner.gate == OwnerReadsAndWrites {
		readMatches = append(slices.Clone(systemMatches), querygen.Match{Column: r.owner.name})
	}

	writeMatches := systemMatches
	if r.owner != nil {
		writeMatches = append(slices.Clone(systemMatches), querygen.Match{Column: r.owner.name})
	}

	r.asOwner = r.renderStatements(nullable, readMatches, writeMatches)
	r.asSystem = r.renderStatements(nullable, systemMatches, systemMatches)

	return r.renderLookups()
}

// renderStatements builds one variant: the reads keyed on readMatches and the
// writes on writeMatches.
func (r *Resource[T]) renderStatements(nullable []string, readMatches, writeMatches []querygen.Match) statements {
	rendered := statements{
		matches:       readMatches,
		listsByLookup: map[string]querygen.Bound{},
		get:           r.generator.BoundGet(r.def.Table, r.columnNames, readMatches...),
		exists:        r.generator.BoundExists(r.def.Table, r.columnNames, readMatches...),
		list:          r.generator.BoundList(r.def.Table, r.columnNames, readMatches...),
		update:        r.generator.BoundUpdate(r.def.Table, r.columnNames, r.updateColumns, nullable, writeMatches...),
	}

	if r.softDeletes {
		rendered.archive = r.generator.BoundArchive(r.def.Table, writeMatches...)
	}

	return rendered
}

// scopeMatches is the predicate every statement carries whatever the actor: the
// tenancy scope, where the table has a column for one.
func (r *Resource[T]) scopeMatches() []querygen.Match {
	if r.scope == nil {
		return nil
	}

	return []querygen.Match{{Column: r.scope.name}}
}

// internal returns the statements the store's own re-reads are served by: the
// ones that carry no owner predicate.
//
// A read that follows a write it just performed is not a consumer read and owes
// no second gate — the write already passed the first one, and applying the
// owner predicate again would mean a create whose row names another author came
// back as "no such row" from a statement that had just inserted it. The scope
// predicate is still there, because the row was written into the scope the call
// named and that has not stopped being true.
func (r *Resource[T]) internal() *statements {
	return &r.asSystem
}

// as returns the statements this actor's calls are served by.
func (r *Resource[T]) as(actor Actor) *statements {
	if actor.IsSystem() {
		return &r.asSystem
	}

	return &r.asOwner
}

// refusePredicateColumn refuses a lookup column the store already binds a
// predicate on for itself.
//
// The two are the tenancy scope, which every read carries, and an owner that
// gates reads. A lookup on either renders a second predicate on the same column,
// and both of them bind under that column's name — so the one argument map holds
// one value, and the value a caller supplied is the one that lands. The gate is
// still in the statement and no longer gates anything.
//
// The owner is only refused where it gates reads. Under OwnerWrites a read
// carries no owner predicate at all, so a lookup on that column is the ordinary
// keyed read it looks like: every author's comments on one reference is the
// question the application actually asks.
func (r *Resource[T]) refusePredicateColumn(column string) error {
	if r.scope != nil && column == r.scope.name {
		return platformerrors.Wrapf(ErrLookupOnPredicateColumn,
			"resources: %s lookup names %q, which is the tenancy scope every read is already keyed on",
			r.def.Name, column)
	}

	if r.owner != nil && r.owner.gate == OwnerReadsAndWrites && column == r.owner.name {
		return platformerrors.Wrapf(ErrLookupOnPredicateColumn,
			"resources: %s lookup names %q, which gates this resource's reads — declare OwnerWrites if it should not",
			r.def.Name, column)
	}

	return nil
}

// renderLookups builds the keyed list statements — one per declared lookup, per
// variant — and the bulk archive, which has only the one.
func (r *Resource[T]) renderLookups() error {
	for _, lookup := range r.def.Lookups {
		var keyed []querygen.Match

		for _, column := range lookup.columns {
			if _, ok := r.columnsByName[column]; !ok {
				return platformerrors.Wrapf(ErrUnknownColumn, "resources: lookup names column %q", column)
			}

			if err := r.refusePredicateColumn(column); err != nil {
				return err
			}

			keyed = append(keyed, querygen.Match{Column: column})
		}

		key := lookup.key()

		if _, duplicate := r.asOwner.listsByLookup[key]; duplicate {
			return platformerrors.Wrapf(ErrDuplicateColumn, "resources: lookup %q declared twice", key)
		}

		for _, rendered := range []*statements{&r.asOwner, &r.asSystem} {
			rendered.listsByLookup[key] = r.generator.BoundList(
				r.def.Table, r.columnNames, append(slices.Clone(rendered.matches), keyed...)...)
		}

		if !r.softDeletes {
			continue
		}

		// The cascade is the system actor's alone — ArchiveMatching says so —
		// so it is rendered once, from the statements that carry no owner.
		bulk, err := r.generator.BoundArchiveMatching(
			r.def.Table, append(slices.Clone(r.asSystem.matches), keyed...)...)
		if err != nil {
			return err
		}

		r.archiveMatchingByLookup[key] = bulk
	}

	return nil
}
