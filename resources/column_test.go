package resources_test

import (
	"errors"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v12/database/dialect"
	"github.com/primandproper/platform-go/v12/resources"
	"github.com/primandproper/platform-go/v12/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// targetType is a named string, the shape an application's enum column takes in
// Go. A match against one is written as a plain string more often than not, and
// both spellings are the same value to a driver.
type targetType string

// note is a row with a named-string column, a nullable one, and a numeric one,
// for the match checking below.
type note struct {
	CreatedAt  time.Time
	ArchivedAt *time.Time
	Body       *string
	ID         string
	Target     targetType
	Ordinal    int
}

func noteDefinition() resources.Definition[note] {
	return resources.Definition[note]{
		Name:  "note",
		Table: "notes",
		Columns: []resources.Column[note]{
			resources.ID(func(n *note) *string { return &n.ID }),
			resources.Field("target", func(n *note) *targetType { return &n.Target }),
			resources.Field("body", func(n *note) **string { return &n.Body }),
			resources.Field("ordinal", func(n *note) *int { return &n.Ordinal }),
			resources.Field("created_at", func(n *note) *time.Time { return &n.CreatedAt }),
			resources.Field("archived_at", func(n *note) **time.Time { return &n.ArchivedAt }),
		},
		Scoping: resources.Unscoped,
		Lookups: []resources.Lookup{
			resources.On("target"),
			resources.On("body"),
			resources.On("ordinal"),
			resources.On("created_at"),
		},
	}
}

// TestColumn_MatchValues pins what a match's value may be, which is the check
// that stands in for the type parameter Match deliberately does not have.
func TestColumn_MatchValues(T *testing.T) {
	T.Parallel()

	store := newNoteStore(T)

	cases := map[string]struct {
		value    any
		column   string
		accepted bool
	}{
		"a string for a text column":                   {column: "target", value: "recipes", accepted: true},
		"the column's own named type":                  {column: "target", value: targetType("recipes"), accepted: true},
		"a string for a nullable text column":          {column: "body", value: "hello", accepted: true},
		"nil for a nullable column":                    {column: "body", value: nil, accepted: true},
		"a time for a timestamp column":                {column: "created_at", value: time.Now().UTC(), accepted: true},
		"a number for a numeric column":                {column: "ordinal", value: 3, accepted: true},
		"a number for a text column":                   {column: "target", value: 3, accepted: false},
		"a string for a numeric column":                {column: "ordinal", value: "3", accepted: false},
		"a time for a text column":                     {column: "target", value: time.Now().UTC(), accepted: false},
		"nil for a column that cannot be null":         {column: "target", value: nil, accepted: false},
		"a string for a column that holds a timestamp": {column: "created_at", value: "yesterday", accepted: false},
	}

	for name, testCase := range cases {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := store.List(t.Context(), tenancy.Global(), alice(), nil, resources.By(testCase.column, testCase.value))

			if testCase.accepted {
				test.False(t, errors.Is(err, resources.ErrMatchTypeMismatch),
					test.Sprintf("%v", err))

				return
			}

			test.ErrorIs(t, err, resources.ErrMatchTypeMismatch)
		})
	}
}

func TestColumn_Name(T *testing.T) {
	T.Parallel()

	test.EqOp(T, "id", resources.ID(func(n *note) *string { return &n.ID }).Name())
	test.EqOp(T, "body", resources.Field("body", func(n *note) **string { return &n.Body }).Name())
}

// newNoteStore builds a store over the notes declaration and the table it
// describes.
func newNoteStore(t *testing.T) *resources.Store[note] {
	t.Helper()

	client := newSQLiteClient(t)

	mustExec(t, client, `
CREATE TABLE IF NOT EXISTS notes (
    id TEXT NOT NULL PRIMARY KEY,
    target TEXT NOT NULL,
    body TEXT,
    ordinal INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    archived_at DATETIME
);`)

	resource, err := resources.Define(dialect.SQLite, noteDefinition())
	must.NoError(t, err)

	store, err := resources.NewStore(resource, client)
	must.NoError(t, err)

	return store
}
