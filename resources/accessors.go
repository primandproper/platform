package resources

import (
	"github.com/primandproper/platform-go/v12/database/querygen"
	platformerrors "github.com/primandproper/platform-go/v12/errors"
	"github.com/primandproper/platform-go/v12/tenancy"
)

// Match is one equality predicate a keyed read or cascade is bound to: a column
// and the value it must equal.
type Match struct {
	// Value is bound, never interpolated.
	//
	// It is an any, and what that costs and why it is paid is Column.accepts:
	// a typed value would mean a typed Column, which cannot sit in the column
	// list a declaration is. The value is checked against the column's field
	// type on the way in instead.
	Value any
	// Column names the column. It has to belong to a declared Lookup — see
	// ErrUndeclaredLookup.
	Column string
}

// By builds a match on one column.
func By(column string, value any) Match {
	return Match{Column: column, Value: value}
}

// matchKey renders a match set as the same order-independent identity Lookup.key
// produces, so a set can be looked up by what it matches on rather than by the
// order it was written in.
func matchKey(matches []Match) string {
	lookup := Lookup{columns: make([]string, 0, len(matches))}
	for i := range matches {
		lookup.columns = append(lookup.columns, matches[i].Column)
	}

	return lookup.key()
}

// checkMatches refuses a match naming a column this resource does not have, or
// carrying a value that column cannot hold.
//
// Both are programming errors, and both are the kind that otherwise surface as
// something else: an unknown column becomes an argument nothing binds, and a
// value of the wrong shape becomes a driver's complaint about a placeholder
// number, from a call site that number does not identify.
func (r *Resource[T]) checkMatches(matches []Match) error {
	for i := range matches {
		match := &matches[i]

		column, ok := r.columnsByName[match.Column]
		if !ok {
			return platformerrors.Wrapf(ErrUnknownColumn, "resources: %s has no column %q", r.def.Table, match.Column)
		}

		if !column.accepts(match.Value) {
			return platformerrors.Wrapf(ErrMatchTypeMismatch,
				"resources: %s.%s holds %s, and the match supplied a %T",
				r.def.Table, column.name, column.valueType, match.Value,
			)
		}
	}

	return nil
}

// bindMatches writes a match set's values into an argument map, under the column
// names the statements bind them by.
//
// It is one function rather than two loops because both callers are binding the
// same set into the same names, and a keyed read and the cascade over the same
// lookup have to agree about which rows they are about.
func bindMatches(values map[string]any, matches []Match) {
	for i := range matches {
		values[matches[i].Column] = matches[i].Value
	}
}

// listFor resolves the statement for a match set, refusing one no Lookup
// declared.
func (r *Resource[T]) listFor(actor Actor, matches []Match) (querygen.Bound, error) {
	rendered := r.as(actor)

	if len(matches) == 0 {
		return rendered.list, nil
	}

	if err := r.checkMatches(matches); err != nil {
		return querygen.Bound{}, err
	}

	statement, ok := rendered.listsByLookup[matchKey(matches)]
	if !ok {
		return querygen.Bound{}, platformerrors.Wrapf(ErrUndeclaredLookup, "resources: %s has no lookup on %s", r.def.Name, matchKey(matches))
	}

	return statement, nil
}

// archiveMatchingFor resolves the cascade statement for a match set.
func (r *Resource[T]) archiveMatchingFor(matches []Match) (querygen.Bound, error) {
	if len(matches) == 0 {
		return querygen.Bound{}, platformerrors.Wrapf(ErrUndeclaredLookup, "resources: a %s cascade needs matches, or it would archive the table", r.def.Name)
	}

	if err := r.checkMatches(matches); err != nil {
		return querygen.Bound{}, err
	}

	statement, ok := r.archiveMatchingByLookup[matchKey(matches)]
	if !ok {
		return querygen.Bound{}, platformerrors.Wrapf(ErrUndeclaredLookup, "resources: %s has no lookup on %s", r.def.Name, matchKey(matches))
	}

	return statement, nil
}

// getManyFor renders the set read for a given number of ids.
//
// It is the one statement this package renders per call rather than at
// construction, and the reason is arity: a set of n ids needs n placeholders on
// a dialect with no array type, so a statement rendered for three cannot be
// executed with four. Postgres binds the set as one array and would render the
// same text every time — but rendering it there at construction and here
// otherwise would be two paths where one suffices, and the rendering is a
// handful of Sprintf calls against a column list that is already assembled.
func (r *Resource[T]) getManyFor(actor Actor, count int) (querygen.Bound, error) {
	return r.generator.BoundGetMany(r.def.Table, r.columnNames, count, r.as(actor).matches...)
}

// scanTargets returns the pointers a read scans into, in the order the SELECT
// lists the columns.
//
// The order is the declaration's, and it is the same slice the statements were
// rendered from, so a column added to the declaration lands in the projection
// and in the scan together.
func (r *Resource[T]) scanTargets(row *T) []any {
	targets := make([]any, 0, len(r.columns))
	for _, column := range r.columns {
		targets = append(targets, column.ref(row))
	}

	return targets
}

// rowValues reads every column off a row into an argument map.
//
// The map is keyed by column name and holds a pointer to each field, which is
// what both a bind and a scan want: database/sql dereferences a pointer when
// binding and allocates through one when scanning, so one accessor serves both.
func (r *Resource[T]) rowValues(row *T) map[string]any {
	values := make(map[string]any, len(r.columns))
	for _, column := range r.columns {
		values[column.name] = column.ref(row)
	}

	return values
}

// idOf reads a row's id.
func (r *Resource[T]) idOf(row *T) string {
	if row == nil {
		return ""
	}

	return r.id.str(row)
}

// setID assigns a row's id, for the mint on create.
func (r *Resource[T]) setID(row *T, id string) {
	if target, ok := r.id.ref(row).(*string); ok {
		*target = id
	}
}

// ownerOf reads a row's owner, or the empty string for a resource that has none.
func (r *Resource[T]) ownerOf(row *T) string {
	if row == nil || r.owner == nil {
		return ""
	}

	return r.owner.str(row)
}

// setScope assigns a row's scope from the one the call named, so a create cannot
// write a row into a tenant the caller did not ask for.
func (r *Resource[T]) setScope(row *T, scope tenancy.Scope) {
	if r.scope == nil {
		return
	}

	if target, ok := r.scope.ref(row).(*tenancy.Scope); ok {
		*target = scope
	}
}
