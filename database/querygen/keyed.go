package querygen

import "fmt"

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

// Match is a predicate on one column, for a read keyed on something other than
// the row's own id — comments on one reference, signups for one waitlist, or
// the whole key of a table whose primary key is natural rather than a surrogate
// id — and for the guards a write puts its own correctness on.
//
// It is a column name rather than rendered SQL because the statements it lands
// in render it more than once: a list query carries its predicates in the SELECT
// and again in each of the two count subqueries beside it. A caller handing over
// finished SQL would have to know how many times its argument was about to
// appear, which is a property of the assembled statement rather than of the
// predicate. Handing over the column instead leaves that to whatever renders the
// finished text.
//
// What the column is compared against is [Match.Against] — a bound argument by
// default, and one of a small closed set of things a statement owns otherwise.
// See [Comparand].
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
	//
	// Only the three comparands that bind anything read it — see [Comparand].
	// Naming an argument that a NULL, empty-string or server-clock comparison
	// has nowhere to put is ErrArgumentlessMatch rather than dead text in a
	// statement.
	Arg string
	// Against is what Column is compared against. The zero value is the bound
	// argument, which is what every keyed read wants; the rest are the guard
	// forms — see [Comparand].
	Against Comparand
	// Exclude inverts the predicate: the rows matched are the ones the
	// uninverted form would have left out.
	//
	// It is a field on Match rather than a second type because the two are the
	// same predicate over the same comparand, differing in one operator, and a
	// caller assembling a mixed key writes one slice either way. The read that
	// wants it against a bound value is the one looking for another row like
	// this one — the remaining live membership when the default is being
	// removed — where the excluded value is as much a part of the key as the
	// included ones.
	//
	// It inverts every comparand rather than only the bound one, and each
	// inversion is a complement rather than a different question: IS NULL
	// becomes IS NOT NULL, `= ''` becomes the not-empty guard, and a clock
	// comparison flips from "at or before now" to "after now". So a guard and
	// the rows it refuses are one Match with one bool between them, which is
	// what keeps "unexpired" and "expired" from being two spellings that can
	// come to disagree about the boundary.
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

// Comparand is what a [Match] compares its column against.
//
// The zero value is a bound argument, which is the predicate this package
// started with and still the one nearly every statement wants. The rest are the
// guard vocabulary — the things a statement owns rather than takes from its
// caller — and the set is closed on purpose. Each member is a shape whose
// meaning is the same on all three dialects and whose spelling this package can
// therefore promise; a caller needing something outside it is describing a
// statement that has to be checked by a person, not one this package should
// learn to guess at.
//
// A guard is not decoration. The reason MarkUserTwoFactorSecretVerified names
// [EmptyString] and [NoValue] is that a replayed verification must write
// nothing, and the reason a token consumption names [CurrentTime] and [NoValue]
// is that an expired or already-redeemed token must not be spendable. Each
// reports zero rows when it loses, which is the answer the caller acts on.
type Comparand int

const (
	// BoundArgument compares the column against a value the caller binds:
	// `column = sqlc.arg(name)`, or `<>` under [Match.Exclude].
	BoundArgument Comparand = iota
	// NoValue compares the column against NULL: `column IS NULL`, or IS NOT
	// NULL under [Match.Exclude]. It binds nothing.
	//
	// It is spelled as its own comparand rather than as a bound NULL because
	// `column = NULL` is not false, it is unknown — the predicate every SQL
	// dialect agrees matches no row, including the rows it was meant to match.
	// A nullable stamp is how this module records that something has not
	// happened yet: an unproven secret, an unredeemed token, a key not yet
	// shredded, a row not yet archived. Guarding on it is what makes the write
	// that does the thing happen exactly once.
	NoValue
	// EmptyString compares the column against the empty string: `column = ''`,
	// or the not-empty guard `column <> ''` under [Match.Exclude]. It binds
	// nothing.
	//
	// The empty string is this module's sentinel for a TEXT NOT NULL column
	// holding nothing yet — an outstanding verification token that has been
	// cleared, a two-factor secret that was never issued — so the not-empty
	// guard is "this fact exists" without a second column to record it in.
	//
	// The literal is the statement's own rather than a bound value on purpose:
	// there is exactly one empty string, so binding it would be an argument
	// every caller had to supply and none could get right in more than one way,
	// and a guard that took its own sentinel from its caller would be one a
	// caller could disarm by leaving the argument unset.
	EmptyString
	// CurrentTime compares the column against the server's clock: `column <=
	// CURRENT_TIMESTAMP`, or `column > CURRENT_TIMESTAMP` under
	// [Match.Exclude]. It binds nothing.
	//
	// The uninverted form is the sweep — expired, elapsed, due — and the
	// inverted one is the guard a consumption puts on itself: still live at the
	// moment the row is claimed. Both are the server's clock rather than the
	// application's, for the reason [NowExpression] gives: a row's timestamps
	// and the comparison against them have to come from one clock, or two
	// application instances a second apart decide differently about the same
	// row.
	//
	// The boundary is inclusive on the expired side, so a row whose deadline is
	// exactly now is past it. That is the reading that leaves no instant at
	// which a row is neither live nor expired.
	CurrentTime
	// BoundInstant compares the column against an instant the caller binds:
	// `column <= sqlc.arg(name)`, or `column > sqlc.arg(name)` under
	// [Match.Exclude].
	//
	// It is [CurrentTime] with the clock moved, and nothing else: the same two
	// directions, the same inclusive boundary on the elapsed side, the same
	// complement between them. What differs is whose reading of the time
	// decides, and that is a decision about the column rather than a
	// convenience. A deadline the database wrote is compared against the
	// database's clock; a deadline the application stamped and handed over is
	// compared against the clock that stamped it, or "issued for fifteen
	// minutes" and "expired" measure two different fifteen minutes.
	//
	// The instant is also not always now. A sweep run at a horizon its caller
	// chose — everything dead for an hour, rather than everything dead —
	// collects only rows nothing is still deciding about, which is the one
	// thing [CurrentTime] cannot say: there is no argument to move.
	//
	// It binds a required argument rather than a nullable one, because an
	// unset horizon is not a wider sweep or a relaxed guard, it is a
	// comparison against NULL that matches no row at all.
	BoundInstant
	// OptionalArgument compares the column against an argument the caller may
	// leave unset: `column = COALESCE(sqlc.narg(name), '')`, or `<>` under
	// [Match.Exclude].
	//
	// It is the presence-conditional predicate, and it is one static statement
	// rather than two texts assembled per call. The excluded form is the one
	// with callers: a uniqueness check that must not collide with the row it is
	// about to update excludes that row's id, and the same check at creation
	// time excludes nothing — so the argument is absent, the COALESCE yields
	// the empty string, and the predicate excludes an id no row has.
	//
	// That correctness rests on the same fact [Generator.CursorCondition]
	// rests on: no id is empty. A column whose domain includes the empty string
	// is a column this comparand cannot speak for, because an unset argument
	// would then name a row.
	OptionalArgument
)

// String names the comparand, for the panic messages the misuse checks raise.
func (c Comparand) String() string {
	switch c {
	case BoundArgument:
		return "bound argument"
	case NoValue:
		return "NULL"
	case EmptyString:
		return "the empty string"
	case CurrentTime:
		return "the current time"
	case BoundInstant:
		return "a bound instant"
	case OptionalArgument:
		return "an optional bound argument"
	default:
		return fmt.Sprintf("unknown comparand %d", int(c))
	}
}

// binds reports whether this comparand takes an argument from the caller, which
// is what decides whether [Match.Arg] means anything.
func (c Comparand) binds() bool {
	return c == BoundArgument || c == BoundInstant || c == OptionalArgument
}

// operator returns the comparison this match renders for the comparands whose
// two directions are spelled `=` and `<>`, which is every one of them but NULL
// and the two that compare against a time.
func (m Match) operator() string {
	if m.Exclude {
		return "<>"
	}

	return "="
}

// orderedOperator returns the comparison the two time comparands render, which
// is an ordering rather than an equality: at or before the instant, and its
// complement, strictly after it.
//
// One function for both of them, because where the boundary falls is one
// decision — a row whose deadline is exactly the instant has elapsed — and a
// second copy of it could come to put it on the other side for whichever
// comparand nobody was looking at. That is a gap in which a row is neither
// live nor collectable, and it is one row wide.
func (m Match) orderedOperator() string {
	if m.Exclude {
		return ">"
	}

	return "<="
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
		Content:    g.getStatement(table, columns, "", Read{}, extra...),
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
		Content:    g.getStatement(table, columns, "", read, extra...),
	}
}

// ExistsQuery renders the existence check for one row by id, plus any extra
// predicate columns. It reports what GetQuery's statement would find without
// reading it.
func (g *Generator) ExistsQuery(name, table string, columns []string, extra ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: OneType},
		Content:    g.existsStatement(table, columns, "", extra...),
	}
}

// ListQueries renders both directions of a list query carrying extra equality
// predicates.
//
// It is listStatement — the same function StandardCRUD's list query comes from,
// with the matches where WithOwnership's column goes — so the filter window, the
// archived toggle, the cursor and the two counts are not merely the same ones a
// generated list gets, they are the same code path. A keyed read filters exactly
// as an unkeyed one does because there is nothing that could make it not.
//
// It returns both directions, under name and [DescendingName] of it, for the
// reason StandardCRUD's list is one entry in its enum: a corpus carrying only
// the ascending half of a list is a store that answers sortBy=desc with an
// ascending page, which is the failure this pair exists to make unspellable.
// The two statements differ in their cursor comparison and their ORDER BY and
// in nothing else — same projection, same predicates, same counts.
//
// Both names must be unique across the consumer's whole sqlc package, as every
// QueryAnnotation.Name must.
func (g *Generator) ListQueries(name, table string, columns []string, matches ...Match) []*Query {
	return []*Query{
		{
			Annotation: QueryAnnotation{Name: name, Type: ManyType},
			Content:    g.listStatement(table, columns, "", nil, Ascending, matches...),
		},
		{
			Annotation: QueryAnnotation{Name: DescendingName(name), Type: ManyType},
			Content:    g.listStatement(table, columns, "", nil, Descending, matches...),
		},
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
