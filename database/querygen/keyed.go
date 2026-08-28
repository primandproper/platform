package querygen

// The statements StandardCRUD emits key a row on its own id and, where the
// caller named one, on an ownership column. That covers a table whose primary
// key is a surrogate id and whose reads are all of the same shape, and it covers
// nothing else: a get keyed on a natural key, a list keyed on a reference, an
// update guarded by the value it is replacing, a read projecting one column.
//
// The forms below are those variants, in the same canonical sqlc spelling the
// standard set is emitted in. Each calls the statement function StandardCRUD
// calls, with the matches where WithOwnership's column goes, so a variant is
// the standard statement with more predicates rather than a second rendering of
// one — the filter window, the archived toggle, the cursor and the two counts
// are the same code path, and there is no pair of renderers to drift.
//
// They are one per statement rather than one call returning a set. StandardCRUD
// answers a generator binary asking "what queries does this table need", where
// the set is the unit and a table gets all of it. A store's corpus asks
// something narrower and per-statement: this table's reads are open within its
// scope but only its owner may write, so the get names one predicate column and
// the update names two. Expressed as one call over one options struct that would
// be a set of per-query overrides; expressed as six calls it is six argument
// lists.
//
// Each takes the name, because a query file's names have to be unique across the
// consumer's whole sqlc package rather than merely within one file, and nothing
// here knows what else that package holds. None of them takes an ownership
// column: a variant's predicates are its matches, which is where the owner goes.
//
// None of them requires an id column, which is where they part company with
// StandardCRUD. A table whose primary key is (subject_type, subject_id) names
// both in Match values and addresses a row exactly; the id predicate is rendered
// only when the column list has one, the same way every other predicate here is.
// What such a table cannot have is the paged list, whose cursor is the id — see
// ErrMissingIDColumn and the package comment. A single-row statement with no id
// and no Match keys on nothing at all, and panics with ErrUnaddressableRow
// rather than addressing every row in the table.

// Match is an equality predicate on one column, for a read keyed on something
// other than the row's own id — comments on one reference, signups for one
// waitlist, or the whole key of a table whose primary key is natural rather than
// a surrogate id.
//
// It is a column name rather than rendered SQL because the statements it lands
// in render it more than once: a list query carries its predicates in the SELECT
// and again in each of the two count subqueries beside it. A caller handing over
// finished SQL would have to know how many times its argument was about to
// appear, which is a property of the assembled statement rather than of the
// predicate. Handing over the column instead leaves that to whatever renders the
// finished text.
type Match struct {
	// Column is the column matched. It is bound, never interpolated, so its
	// value needs no escaping; the name itself is interpolated and is therefore
	// restricted — see dialect.ValidIdentifier.
	Column string
	// Arg names the argument the column is compared against, for a predicate
	// whose value is not simply "this column's value". It defaults to Column,
	// which is what every keyed read wants: a get by account binds the account
	// under belongs_to_account and nothing is clearer than that.
	//
	// A guarded write is what needs the other spelling. Naming the current
	// owner in a transfer's predicate as well as the new one in its SET is the
	// whole mechanism that stops two concurrent transfers from both succeeding,
	// and both halves are the owner column — so under one argument name the
	// statement would set the column to the value it was requiring it to
	// already hold, which is legal SQL that guards nothing. Arg is what makes
	// the two ends of that comparison two arguments.
	//
	// It is a name rather than a value, and it is interpolated into the
	// statement the way Column is, so it is restricted the same way.
	Arg string
	// Exclude inverts the predicate: the rows matched are the ones whose column
	// does not hold the bound value.
	//
	// It is a field on Match rather than a second type because the two are the
	// same predicate over the same bound argument, differing in one operator,
	// and a caller assembling a mixed key writes one slice either way. The read
	// that wants it is the one looking for another row like this one — the
	// remaining live membership when the default is being removed — where the
	// excluded value is as much a part of the key as the included ones.
	Exclude bool
}

// argument returns the name this match binds through: Arg where the caller gave
// one, and the column otherwise.
func (m Match) argument() string {
	if m.Arg != "" {
		return m.Arg
	}

	return m.Column
}

// Read is what a keyed read returns, and how it chooses when the key admits
// more than one row.
//
// It exists because a keyed read's projection and its predicates come from
// different lists, which the standard get's do not. The get projects the table
// and keys on the table's id, so one column list says both things. A read of
// the creation time the database assigned projects one column and keys on the
// id; a read of the membership between a user and an account projects every
// column and keys on neither its own id nor a filter. Deriving both from one
// list would mean a narrow projection silently dropping the archived predicate
// that the same list carries.
//
// The zero value is the standard get: the whole column list projected, and no
// ordering, because the key names one row.
type Read struct {
	// Order names a column the read sorts ascending by.
	//
	// In a single-row read it is what picks the row: the statement orders by it
	// and takes the first, which is for the key that admits more than one row —
	// "another live membership for this user" — where without it the row
	// answered is whichever the planner reached first, and a :one statement
	// discards the rest after dragging them across the wire. Empty is a key
	// that identifies a row, which needs neither.
	//
	// In a batched read it is the tie-break inside one key's rows, since that
	// statement's first ordering term is the keyed column itself — see
	// [Generator.SetReadQuery].
	Order string
	// Projection is the columns the SELECT lists, in order. Empty projects the
	// column list the statement was rendered from.
	Projection []string
}

// projecting returns the columns this read lists, which is the statement's own
// column list unless the read narrows it.
func (r Read) projecting(columns []string) []string {
	if len(r.Projection) == 0 {
		return columns
	}

	return r.Projection
}

// GetQuery renders the read of one row by id, plus any extra predicate columns.
func (g *Generator) GetQuery(name, table string, columns []string, extra ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: OneType},
		Content:    getStatement(table, columns, "", Read{}, extra...),
	}
}

// CreateQuery renders the insert. insertColumns is what the caller supplies —
// ForInsert over the table's columns — and nullable names those whose value may
// be NULL.
//
// StandardCRUD emits this statement too, for the tables it can emit anything
// for. This is the form a table it cannot serve needs: an INSERT keys on
// nothing, so it is the one statement a natural-key table wants unchanged from
// the standard set while every other one it wants keyed on that natural key.
// Without it such a table's corpus would have five statements sqlc checks and a
// sixth nobody could render.
//
// It is annotated :exec rather than :execrows, like the standard create,
// because a failed insert raises rather than returning zero and there is no
// count worth reading.
func (g *Generator) CreateQuery(name, table string, insertColumns, nullable []string) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ExecType},
		Content:    createStatement(table, insertColumns, nullable),
	}
}

// ReadQuery renders a keyed read that is not the standard get: one that returns
// a narrower projection than the table, or that keys on something other than
// the row's own id, or both.
//
// columns stays the table's shape — what the id and archived predicates are
// derived from — and read says what comes back. A table keyed on a natural key
// while still carrying an id leaves the id out of columns and names it in
// read.Projection, which is the same idiom a table with no id at all already
// uses, with the projection now able to say so.
func (g *Generator) ReadQuery(name, table string, columns []string, read Read, extra ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: OneType},
		Content:    getStatement(table, columns, "", read, extra...),
	}
}

// ExistsQuery renders the existence check for one row by id, plus any extra
// predicate columns. It reports what GetQuery's statement would find without
// reading it.
func (g *Generator) ExistsQuery(name, table string, columns []string, extra ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: OneType},
		Content:    existsStatement(table, columns, "", extra...),
	}
}

// ListQuery renders a list query carrying extra equality predicates.
//
// It is listStatement — the same function StandardCRUD's list query comes from,
// with the matches where WithOwnership's column goes — so the filter window, the
// archived toggle, the cursor and the two counts are not merely the same ones a
// generated list gets, they are the same code path. A keyed read filters exactly
// as an unkeyed one does because there is nothing that could make it not.
func (g *Generator) ListQuery(name, table string, columns []string, matches ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ManyType},
		Content:    g.listStatement(table, columns, "", nil, matches...),
	}
}

// UpdateQuery renders the update: the named columns assigned, last_updated_at
// stamped, keyed on the id and any extra predicate columns.
//
// updateColumns is what this statement assigns rather than what the table lets
// anyone assign, which is what makes it the field-specific writes as well as the
// conventional one. A password change names the hash, the forced-change flag and
// the stamp that goes with them; a status move names the status and its
// explanation. Each is one statement whose SET list is written down, rather than
// a whole-row write a caller has to remember not to reach for with a struct
// whose credential fields it blanked.
//
// The extra matches are the guard. A write that must not race another one names
// the value it requires the row to still hold — the token a verification link
// carries, the owner a transfer is moving away from, the pending status an
// answer replaces — and the row count reports whether it was the one that won.
// Where such a guard names a column the SET list also assigns, the two ends need
// two argument names or the statement sets the column to the value it is
// requiring it to already hold; that is what Match.Arg is for.
//
// A caller wanting to move a row between owners without guarding also wants that
// column out of updateColumns, which is what ForUpdate's exceptions are for —
// and a table keyed on a natural key wants every column of that key out of it,
// since ForUpdate subtracts the id and knows nothing of the rest.
//
// It is annotated :execrows rather than :exec, like the standard update, because
// the count is the answer: a guarded write that matched nothing is how a caller
// learns it lost the race, and an unguarded one that matched nothing is how it
// learns the row was already gone.
func (g *Generator) UpdateQuery(name, table string, columns, updateColumns, nullable []string, extra ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ExecRowsType},
		Content:    g.updateStatement(table, columns, updateColumns, "", nullable, extra...),
	}
}

// ArchiveQuery renders the soft delete of one row by id, plus any extra
// predicate columns.
//
// It takes the column list the other single-row statements take, because its
// predicates are derived from one like theirs: the id predicate appears only for
// a table that has an id, and the archived_at IS NULL that makes archiving
// idempotent appears only for a table whose column list says the column is
// there. A list omitting archived_at therefore yields an archive that restamps
// an already-archived row and reports it as a write — so a caller passes the
// table's columns rather than a subset chosen for this call.
func (g *Generator) ArchiveQuery(name, table string, columns []string, extra ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ExecRowsType},
		Content:    g.archiveStatement(table, columns, "", extra...),
	}
}
