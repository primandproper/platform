package resources_test

import (
	"testing"
	"time"

	"github.com/primandproper/platform-go/v12/database/dialect"
	"github.com/primandproper/platform-go/v12/database/querygen"
	"github.com/primandproper/platform-go/v12/resources"
	"github.com/primandproper/platform-go/v12/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// widget is a conventionally shaped scoped row: an id, a scope, an owner, and
// the three timestamps.
type widget struct {
	CreatedAt     time.Time
	LastUpdatedAt *time.Time
	ArchivedAt    *time.Time
	ID            string
	Name          string
	BelongsToUser string
	Scope         tenancy.Scope
}

func widgetColumns() []resources.Column[widget] {
	return []resources.Column[widget]{
		resources.ID(func(w *widget) *string { return &w.ID }),
		resources.Field("name", func(w *widget) *string { return &w.Name }),
		resources.Scope("scope", func(w *widget) *tenancy.Scope { return &w.Scope }),
		resources.Owner("belongs_to_user", func(w *widget) *string { return &w.BelongsToUser }, resources.OwnerReadsAndWrites),
		resources.Field("created_at", func(w *widget) *time.Time { return &w.CreatedAt }),
		resources.Field("last_updated_at", func(w *widget) **time.Time { return &w.LastUpdatedAt }),
		resources.Field("archived_at", func(w *widget) **time.Time { return &w.ArchivedAt }),
	}
}

func widgetDefinition() resources.Definition[widget] {
	return resources.Definition[widget]{
		Name:    "widget",
		Table:   "widgets",
		Columns: widgetColumns(),
		Lookups: []resources.Lookup{resources.On("name")},
	}
}

func TestDefine(T *testing.T) {
	T.Parallel()

	T.Run("accepts a conventional declaration", func(t *testing.T) {
		t.Parallel()

		resource, err := resources.Define(dialect.Postgres, widgetDefinition())
		must.NoError(t, err)
		test.EqOp(t, "widget", resource.Name())
		test.EqOp(t, "widgets", resource.Table())
	})

	T.Run("refuses a declaration with no id column", func(t *testing.T) {
		t.Parallel()

		def := widgetDefinition()
		def.Columns = def.Columns[1:]

		_, err := resources.Define(dialect.Postgres, def)
		test.ErrorIs(t, err, resources.ErrNoIDColumn)
	})

	T.Run("refuses a column declared twice", func(t *testing.T) {
		t.Parallel()

		def := widgetDefinition()
		def.Columns = append(def.Columns, resources.Field("name", func(w *widget) *string { return &w.Name }))

		_, err := resources.Define(dialect.Postgres, def)
		test.ErrorIs(t, err, resources.ErrDuplicateColumn)
	})

	T.Run("refuses two columns in one role", func(t *testing.T) {
		t.Parallel()

		def := widgetDefinition()
		def.Columns = append(def.Columns,
			resources.Owner("belongs_to_account", func(w *widget) *string { return &w.BelongsToUser }, resources.OwnerWrites))

		_, err := resources.Define(dialect.Postgres, def)
		test.ErrorIs(t, err, resources.ErrConflictingRole)
	})

	T.Run("refuses a scope column on a resource declaring itself unscoped", func(t *testing.T) {
		t.Parallel()

		def := widgetDefinition()
		def.Scoping = resources.Unscoped

		_, err := resources.Define(dialect.Postgres, def)
		test.ErrorIs(t, err, resources.ErrConflictingRole)
	})

	T.Run("refuses a scoped resource with no scope column", func(t *testing.T) {
		t.Parallel()

		def := commentsDefinition()
		def.Scoping = resources.ScopedByColumn

		_, err := resources.Define(dialect.Postgres, def)
		test.ErrorIs(t, err, resources.ErrConflictingRole)
	})

	T.Run("refuses a lookup on the scope column", func(t *testing.T) {
		t.Parallel()

		// The scope is bound from the call's argument and a match is bound under
		// the same column name, so the statement would compare the tenancy
		// predicate against whatever the caller asked for. A resource that could
		// answer one tenant's read with another's rows should not be
		// constructible.
		def := widgetDefinition()
		def.Lookups = append(def.Lookups, resources.On("scope"))

		_, err := resources.Define(dialect.Postgres, def)
		test.ErrorIs(t, err, resources.ErrLookupOnPredicateColumn)
	})

	T.Run("refuses a lookup on an owner that gates reads", func(t *testing.T) {
		t.Parallel()

		// Same collision, one dimension over: under OwnerReadsAndWrites the read
		// carries the owner, so a lookup naming it lets a caller read by naming
		// somebody else.
		def := widgetDefinition()
		def.Lookups = append(def.Lookups, resources.On("belongs_to_user"))

		_, err := resources.Define(dialect.Postgres, def)
		test.ErrorIs(t, err, resources.ErrLookupOnPredicateColumn)
	})

	T.Run("accepts a lookup on an owner that gates only writes", func(t *testing.T) {
		t.Parallel()

		// comments declares On("belongs_to_user") and must keep working: under
		// OwnerWrites a read carries no owner predicate, so there is nothing for
		// the match to collide with. Every author's comments on one reference is
		// the question the application actually asks.
		_, err := resources.Define(dialect.Postgres, commentsDefinition())
		must.NoError(t, err)
	})

	T.Run("refuses a lookup naming a column the table does not have", func(t *testing.T) {
		t.Parallel()

		def := widgetDefinition()
		def.Lookups = []resources.Lookup{resources.On("color")}

		_, err := resources.Define(dialect.Postgres, def)
		test.ErrorIs(t, err, resources.ErrUnknownColumn)
	})

	T.Run("refuses one lookup declared twice", func(t *testing.T) {
		t.Parallel()

		def := widgetDefinition()
		// The same set, written in two orders. A lookup is its columns, not the
		// order somebody wrote them in, so these are one declaration made twice.
		def.Lookups = []resources.Lookup{
			resources.On("name", "created_at"),
			resources.On("created_at", "name"),
		}

		_, err := resources.Define(dialect.Postgres, def)
		test.ErrorIs(t, err, resources.ErrDuplicateColumn)
	})

	T.Run("refuses names it would have to interpolate", func(t *testing.T) {
		t.Parallel()

		def := widgetDefinition()
		def.Table = "widgets; DROP TABLE users"

		_, err := resources.Define(dialect.Postgres, def)
		test.ErrorIs(t, err, dialect.ErrInvalidIdentifier)

		def = widgetDefinition()
		def.Columns = append(def.Columns, resources.Field("name)", func(w *widget) *string { return &w.Name }))

		_, err = resources.Define(dialect.Postgres, def)
		test.ErrorIs(t, err, dialect.ErrInvalidIdentifier)
	})

	T.Run("refuses an empty declaration", func(t *testing.T) {
		t.Parallel()

		_, err := resources.Define(dialect.Postgres, resources.Definition[widget]{})
		must.Error(t, err)
	})

	T.Run("refuses a dialect it cannot render for", func(t *testing.T) {
		t.Parallel()

		_, err := resources.Define(dialect.Dialect("oracle"), widgetDefinition())
		test.ErrorIs(t, err, dialect.ErrUnsupported)
	})

	T.Run("registers the table it serves", func(t *testing.T) {
		t.Parallel()

		registry := querygen.NewRegistry()

		def := widgetDefinition()
		def.Table = "registered_widgets"
		def.Registry = registry

		_, err := resources.Define(dialect.Postgres, def)
		must.NoError(t, err)

		// A table served here emits no generated queries, so a consumer's table
		// list has to come from somewhere else — and this is that somewhere.
		test.True(t, registry.Has("registered_widgets"))
	})

	T.Run("a refused declaration registers nothing", func(t *testing.T) {
		t.Parallel()

		registry := querygen.NewRegistry()

		def := widgetDefinition()
		def.Table = "unservable_widgets"
		def.Registry = registry
		def.Lookups = []resources.Lookup{resources.On("color")}

		_, err := resources.Define(dialect.Postgres, def)
		must.Error(t, err)

		test.False(t, registry.Has("unservable_widgets"))
	})
}

func TestMustDefine(T *testing.T) {
	T.Parallel()

	T.Run("returns the resource for a declaration that checks out", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "widget", resources.MustDefine(dialect.Postgres, widgetDefinition()).Name())
	})

	T.Run("panics on one that does not", func(t *testing.T) {
		t.Parallel()

		def := widgetDefinition()
		def.Lookups = []resources.Lookup{resources.On("color")}

		defer func() {
			must.NotNil(t, recover())
		}()

		resources.MustDefine(dialect.Postgres, def)
	})
}
