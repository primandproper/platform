package querygen

import (
	"regexp"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// gadgetPartsTable is the child table the batched reads below key on: the rows
// that hang off a gadget, read for a whole page of gadgets at once. It is the
// shape every N+1 read collapses into, and it is deliberately not conventional
// — no id, no timestamps — because a child set table usually is not.
const gadgetPartsTable = "gadget_parts"

func gadgetPartsColumns() []string {
	return []string{"gadget_id", "part"}
}

// setArgument matches a reference to a bound set in either spelling, so an
// assertion about where the set sits in a statement does not have to know which
// dialect rendered it.
var setArgument = regexp.MustCompile(`ANY\(sqlc\.arg\([a-z_]+\)::text\[\]\)|sqlc\.slice\([a-z_]+\)`)

func TestGenerator_SetReadQuery(T *testing.T) {
	T.Parallel()

	T.Run("keys on the set and orders by it", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)
			q := g.SetReadQuery("ListGadgetPartsByGadgetIDs", gadgetPartsTable, gadgetPartsColumns(),
				Read{Order: "part"}, SetKey{Column: "gadget_id"})

			test.EqOp(t, "ListGadgetPartsByGadgetIDs", q.Annotation.Name)

			// :many, because a batch of keys is a batch of answers. A caller
			// grouping by the key is the whole point of the shape.
			test.EqOp(t, ManyType, q.Annotation.Type)

			test.StrContains(t, q.Content, g.setPredicate(Qualify(gadgetPartsTable, "gadget_id"), IDsArg),
				test.Sprintf("dialect %q", d))

			// The key first, so one key's rows arrive together, then the
			// tie-break inside the group.
			test.StrContains(t, q.Content,
				"ORDER BY "+Qualify(gadgetPartsTable, "gadget_id")+" ASC, "+Qualify(gadgetPartsTable, "part")+" ASC",
				test.Sprintf("dialect %q", d))
		}
	})

	// The requirement is not cosmetic: on the two dialects with no array type
	// the set expands into one bare placeholder per element, and SQLite numbers
	// a bare marker one past the highest it has seen — so an argument bound
	// after the set collides with an element of it, matches nothing, and
	// reports no error.
	T.Run("renders the set after every other argument", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := For(d).SetReadQuery("ListGadgetsByIDs", keyedTable, keyedColumns(),
				Read{}, SetKey{Column: IDColumn},
				Match{Column: BelongsToAccountColumn})

			set := setArgument.FindStringIndex(q.Content)
			must.NotNil(t, set, must.Sprintf("dialect %q renders no set", d))

			test.False(t, strings.Contains(q.Content[set[1]:], "sqlc."),
				test.Sprintf("dialect %q binds an argument after the set", d))
		}
	})

	T.Run("the column list decides the archived predicate", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)

			withArchived := g.SetReadQuery("ListGadgetsByIDs", keyedTable, keyedColumns(),
				Read{}, SetKey{Column: IDColumn})

			test.StrContains(t, withArchived.Content, Qualify(keyedTable, ArchivedAtColumn)+" IS NULL",
				test.Sprintf("dialect %q", d))

			// A hydration read — who created each of these rows — wants the
			// archived ones too, and says so by handing over a column list
			// without archived_at rather than by a flag on the statement.
			hydrating := g.SetReadQuery("ListGadgetsByIDs", keyedTable, without(keyedColumns(), ArchivedAtColumn),
				Read{Projection: keyedColumns()}, SetKey{Column: IDColumn})

			test.False(t, strings.Contains(hydrating.Content, "IS NULL"),
				test.Sprintf("dialect %q", d))

			// The projection is still the table's, archived_at included: what
			// the column list decided is the predicate, not what comes back.
			test.StrContains(t, hydrating.Content, Qualify(keyedTable, ArchivedAtColumn)+"\n",
				test.Sprintf("dialect %q", d))
		}
	})

	T.Run("binds the set under the caller's argument name", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)

			byDefault := g.SetReadQuery("ListGadgetPartsByGadgetIDs", gadgetPartsTable, gadgetPartsColumns(),
				Read{}, SetKey{Column: "gadget_id"})
			test.StrContains(t, byDefault.Content, IDsArg, test.Sprintf("dialect %q", d))

			named := g.SetReadQuery("ListGadgetPartsByGadgetIDs", gadgetPartsTable, gadgetPartsColumns(),
				Read{}, SetKey{Column: "gadget_id", Arg: "gadget_ids"})
			test.StrContains(t, named.Content, "gadget_ids", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("carries whatever else keys the read", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := For(d).SetReadQuery("ListGadgetsByIDs", keyedTable, keyedColumns(),
				Read{}, SetKey{Column: IDColumn},
				Match{Column: BelongsToAccountColumn})

			// A batched read is still a consumer read, so the scope or owner it
			// is keyed on is a predicate rather than something the caller
			// filters out of the answer.
			test.StrContains(t, q.Content, "sqlc.arg("+BelongsToAccountColumn+")", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("refuses a set that keys on nothing", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			err := recovered(func() {
				_ = For(d).SetReadQuery("ListGadgetsByIDs", keyedTable, keyedColumns(), Read{}, SetKey{})
			})

			must.Error(t, err, must.Sprintf("dialect %q", d))
			test.ErrorIs(t, err, ErrMissingSetColumn, test.Sprintf("dialect %q", d))
			test.StrContains(t, err.Error(), keyedTable, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("refuses an identifier it would interpolate", func(t *testing.T) {
		t.Parallel()

		// The column and the argument name are interpolated into statement
		// text rather than bound, so both are restricted rather than escaped.
		for _, d := range everyDialect() {
			for _, render := range []func(){
				func() {
					_ = For(d).SetReadQuery("X", "gadgets; DROP TABLE gadgets", keyedColumns(), Read{}, SetKey{Column: IDColumn})
				},
				func() {
					_ = For(d).SetReadQuery("X", keyedTable, keyedColumns(), Read{}, SetKey{Column: "id) OR (1=1"})
				},
				func() {
					_ = For(d).SetReadQuery("X", keyedTable, keyedColumns(), Read{}, SetKey{Column: IDColumn, Arg: "ids)::text[]) OR (1=1"})
				},
			} {
				err := recovered(render)

				must.Error(t, err, must.Sprintf("dialect %q", d))
				test.ErrorIs(t, err, dialect.ErrInvalidIdentifier, test.Sprintf("dialect %q", d))
			}
		}
	})
}
