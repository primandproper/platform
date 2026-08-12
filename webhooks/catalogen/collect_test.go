package catalogen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// constantsInSource parses a file's worth of Go and returns what the collector
// makes of it, which is the seam the description convention lives on.
func constantsInSource(t *testing.T, src string) []entry {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "events.go", src, parser.ParseComments|parser.SkipObjectResolution)
	must.NoError(t, err)

	entries, err := constantsIn(file, "events.go", defaultSuffix)
	must.NoError(t, err)

	return entries
}

func TestDescriptions(T *testing.T) {
	T.Parallel()

	T.Run("a doc comment becomes a sentence without the identifier", func(t *testing.T) {
		t.Parallel()

		entries := constantsInSource(t, `package events

const (
	// OrderCreatedEventType fires when an order is placed.
	OrderCreatedEventType = "order.created"
)
`)

		must.SliceLen(t, 1, entries)
		test.EqOp(t, "Fires when an order is placed.", entries[0].description)
	})

	T.Run("a wrapped comment becomes one line", func(t *testing.T) {
		t.Parallel()

		entries := constantsInSource(t, `package events

const (
	// OrderCreatedEventType fires when an order is placed, which is
	// after payment authorization and before fulfillment.
	OrderCreatedEventType = "order.created"
)
`)

		must.SliceLen(t, 1, entries)
		test.EqOp(t, "Fires when an order is placed, which is after payment authorization and before fulfillment.", entries[0].description)
	})

	T.Run("a comment that does not start with the identifier is left alone", func(t *testing.T) {
		t.Parallel()

		entries := constantsInSource(t, `package events

const (
	// Fires when an order is placed.
	OrderCreatedEventType = "order.created"
)
`)

		must.SliceLen(t, 1, entries)
		test.EqOp(t, "Fires when an order is placed.", entries[0].description)
	})

	T.Run("a lone declaration carries its comment on the declaration", func(t *testing.T) {
		t.Parallel()

		entries := constantsInSource(t, `package events

// OrderCreatedEventType fires when an order is placed.
const OrderCreatedEventType = "order.created"
`)

		must.SliceLen(t, 1, entries)
		test.EqOp(t, "Fires when an order is placed.", entries[0].description)
	})

	T.Run("a trailing line comment is the last resort", func(t *testing.T) {
		t.Parallel()

		entries := constantsInSource(t, `package events

const (
	OrderCreatedEventType = "order.created" // Fires when an order is placed.
)
`)

		must.SliceLen(t, 1, entries)
		test.EqOp(t, "Fires when an order is placed.", entries[0].description)
	})

	T.Run("a grouped declaration's comment does not describe every member", func(t *testing.T) {
		t.Parallel()

		// A comment introducing a block says something about the block, not
		// about each event in it, and copying it onto all of them would put
		// prose in the UI that describes none of them.
		entries := constantsInSource(t, `package events

// The events the orders domain publishes.
const (
	OrderCreatedEventType = "order.created"
	OrderShippedEventType = "order.shipped"
)
`)

		must.SliceLen(t, 2, entries)
		test.EqOp(t, "", entries[0].description)
		test.EqOp(t, "", entries[1].description)
	})

	T.Run("an undocumented constant is generated with no description", func(t *testing.T) {
		t.Parallel()

		// A blank label is visible in the UI and fixable by writing the
		// comment; refusing to generate would take the dispatch gate down over
		// prose.
		entries := constantsInSource(t, `package events

const OrderCreatedEventType = "order.created"
`)

		must.SliceLen(t, 1, entries)
		test.EqOp(t, "", entries[0].description)
	})

	T.Run("a comment that is only the identifier describes nothing", func(t *testing.T) {
		t.Parallel()

		// The shape a doc-comment lint rule is satisfied by. Left alone it puts
		// a Go identifier in the subscription UI.
		entries := constantsInSource(t, `package events

// OrderCreatedEventType
const OrderCreatedEventType = "order.created"
`)

		must.SliceLen(t, 1, entries)
		test.EqOp(t, "", entries[0].description)
	})

	T.Run("a directive comment is not prose", func(t *testing.T) {
		t.Parallel()

		entries := constantsInSource(t, `package events

//go:generate something
const OrderCreatedEventType = "order.created"
`)

		must.SliceLen(t, 1, entries)
		test.EqOp(t, "", entries[0].description)
	})
}

func TestCollectedValues(T *testing.T) {
	T.Parallel()

	T.Run("reads the forms an event type is written in", func(t *testing.T) {
		t.Parallel()

		entries := constantsInSource(t, `package events

type EventType string

const (
	BareEventType                 = "bare.event"
	TypedEventType     EventType  = "typed.event"
	ConvertedEventType            = EventType("converted.event")
	ParenthesizedEventType        = ("parenthesized.event")
	RawEventType                  = `+"`raw.event`"+`
)
`)

		values := make([]string, 0, len(entries))
		for _, e := range entries {
			values = append(values, e.eventType)
		}

		test.Eq(t, []string{"bare.event", "typed.event", "converted.event", "parenthesized.event", "raw.event"}, values)
	})

	T.Run("ignores constants without the suffix and blanks", func(t *testing.T) {
		t.Parallel()

		entries := constantsInSource(t, `package events

const (
	OrderCreatedEventType = "order.created"
	SomethingElse         = "not.an.event"
	_                     = "blank.eventtype"
)
`)

		must.SliceLen(t, 1, entries)
		test.EqOp(t, "order.created", entries[0].eventType)
	})

	T.Run("carries the constant name and file for the generated comment", func(t *testing.T) {
		t.Parallel()

		entries := constantsInSource(t, `package events

const OrderCreatedEventType = "order.created"
`)

		must.SliceLen(t, 1, entries)
		test.EqOp(t, "OrderCreatedEventType", entries[0].constName)
		test.EqOp(t, "events.go", entries[0].path)
	})
}

func TestSkipDir(T *testing.T) {
	T.Parallel()

	T.Run("skips what cannot hold this application's domain", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{"vendor", "testdata", ".git", "_scratch"} {
			test.True(t, skipDir(name), test.Sprintf("directory %q", name))
		}
	})

	T.Run("descends into everything else", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{"orders", "identity", "internal", "webhooks"} {
			test.False(t, skipDir(name), test.Sprintf("directory %q", name))
		}
	})
}

func TestStringValue(T *testing.T) {
	T.Parallel()

	T.Run("refuses what it cannot resolve without a type checker", func(t *testing.T) {
		t.Parallel()

		for _, src := range []string{
			"OtherConstant",
			`"a" + "b"`,
			"iota",
			"3",
			`fmt.Sprintf("%s", x)`,
		} {
			expr, err := parser.ParseExpr(src)
			must.NoError(t, err)

			_, ok := stringValue(expr)
			test.False(t, ok, test.Sprintf("expression %q", src))
		}
	})

	T.Run("refuses a non-string literal in a conversion", func(t *testing.T) {
		t.Parallel()

		expr, err := parser.ParseExpr("EventType(3)")
		must.NoError(t, err)

		_, ok := stringValue(expr)
		test.False(t, ok)
	})

	T.Run("reads a string literal", func(t *testing.T) {
		t.Parallel()

		expr, err := parser.ParseExpr(`"order.created"`)
		must.NoError(t, err)

		value, ok := stringValue(expr)
		must.True(t, ok)
		test.EqOp(t, "order.created", value)
	})

	T.Run("is not confused by a nil expression's type", func(t *testing.T) {
		t.Parallel()

		var expr ast.Expr

		_, ok := stringValue(expr)
		test.False(t, ok)
	})
}
