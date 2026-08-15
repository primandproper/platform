package filtering

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The schema is reflected off QueryFilter, so most of what could go wrong with
// it is a tag that says the wrong thing rather than code that computes the wrong
// thing. These tests are written against the constants and the struct itself for
// that reason: a tag holds "250" because a tag cannot hold MaxQueryFilterLimit,
// and the assertion below is what stands in for the compiler noticing.

// jsonNames is every property name QueryFilter's tags declare, in field order.
// It fails on an exported field with no `json` tag rather than skipping it,
// because such a field is one encoding/json would key on its Go name and the
// schema would not mention at all.
func jsonNames(t *testing.T) []string {
	t.Helper()

	typ := reflect.TypeFor[QueryFilter]()

	names := make([]string, 0, typ.NumField())

	for field := range typ.Fields() {
		tag, tagged := field.Tag.Lookup("json")
		if !tagged {
			if field.IsExported() {
				t.Fatalf("QueryFilter.%s is exported and has no json tag", field.Name)
			}

			continue
		}

		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}

		names = append(names, name)
	}

	return names
}

// property reads one property subschema, failing if it is absent or is not an
// object.
func property(t *testing.T, name string) map[string]any {
	t.Helper()

	properties, ok := QueryFilterSchema()["properties"].(map[string]any)
	must.True(t, ok, must.Sprint("schema has no properties object"))

	prop, ok := properties[name].(map[string]any)
	must.True(t, ok, must.Sprintf("schema has no property %q", name))

	return prop
}

func TestQueryFilterSchema(t *testing.T) {
	t.Parallel()

	schema := QueryFilterSchema()

	test.EqOp(t, "object", schema["type"])

	// The struct is closed: encoding/json discards a key it does not know, so a
	// model that invents one is answered with silence rather than an error.
	test.EqOp(t, false, schema["additionalProperties"])

	description, ok := schema["description"].(string)
	test.True(t, ok, test.Sprint("schema has no description"))
	test.NotEq(t, "", description)
}

func TestQueryFilterSchema_PropertiesAreTheTagNames(t *testing.T) {
	t.Parallel()

	properties, ok := QueryFilterSchema()["properties"].(map[string]any)
	must.True(t, ok, must.Sprint("schema has no properties object"))

	want := jsonNames(t)
	got := slices.Sorted(maps.Keys(properties))

	slices.Sort(want)

	// Go field names decode today only because encoding/json falls back to a
	// case-insensitive match against the tag. The keys are the tags so that the
	// fallback is not what the schema is resting on.
	test.Eq(t, want, got)
}

func TestQueryFilterSchema_EveryPropertyIsDescribed(t *testing.T) {
	t.Parallel()

	// A property with no description is one a model has only its name to go on
	// for, which is how "sortBy" gets a column name put in it.
	for _, name := range jsonNames(t) {
		prop := property(t, name)

		description, ok := prop["description"].(string)
		test.True(t, ok, test.Sprintf("%s has no description", name))
		test.NotEq(t, "", description)
	}
}

func TestQueryFilterSchema_NothingIsNullable(t *testing.T) {
	t.Parallel()

	// Every field is a pointer, which the reflector reads as nullable unless
	// told otherwise. It is not: absent is how a filter says it does not filter
	// on something, and null is never emitted for one.
	for _, name := range jsonNames(t) {
		test.NotEq(t, "null", property(t, name)["type"])
	}
}

func TestQueryFilterSchema_SortByIsADirection(t *testing.T) {
	t.Parallel()

	prop := property(t, "sortBy")

	test.EqOp(t, "string", prop["type"])

	// Read off the vars rather than spelled out: renaming a direction has to
	// fail here rather than ship a schema offering one that FromParams rejects.
	enum, ok := prop["enum"].([]any)
	must.True(t, ok, must.Sprint("sortBy has no enum"))
	test.Eq(t, []any{*SortAscending, *SortDescending}, enum)

	fallback, ok := prop["default"].(string)
	must.True(t, ok, must.Sprint("sortBy has no default"))
	test.EqOp(t, *DefaultQueryFilter().SortBy, fallback)
}

func TestQueryFilterSchema_Bounds(t *testing.T) {
	t.Parallel()

	prop := property(t, "maxResponseSize")

	test.EqOp(t, "integer", prop["type"])

	// The tags spell these out because a struct tag cannot name a constant.
	// This is the assertion that keeps the two spellings the same number.
	minimum, ok := prop["minimum"].(float64)
	must.True(t, ok, must.Sprint("maxResponseSize has no minimum"))
	test.EqOp(t, float64(0), minimum)

	maximum, ok := prop["maximum"].(float64)
	must.True(t, ok, must.Sprint("maxResponseSize has no maximum"))
	test.EqOp(t, float64(MaxQueryFilterLimit), maximum)

	size, ok := prop["default"].(float64)
	must.True(t, ok, must.Sprint("maxResponseSize has no default"))
	test.EqOp(t, float64(DefaultQueryFilterLimit), size)
	test.EqOp(t, float64(*DefaultQueryFilter().MaxResponseSize), size)

	// The ceiling is a clamp and not a rejection, which no JSON Schema keyword
	// can say. The description is where it gets said, so it has to keep saying
	// it.
	description, ok := prop["description"].(string)
	must.True(t, ok, must.Sprint("maxResponseSize has no description"))
	test.StrContains(t, description, "clamped")
}

func TestQueryFilterSchema_TimeWindows(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"createdAfter", "createdBefore", "updatedAfter", "updatedBefore"} {
		prop := property(t, name)

		test.EqOp(t, "string", prop["type"])
		test.EqOp(t, "date-time", prop["format"], test.Sprintf("%s is not a timestamp", name))
	}
}

func TestQueryFilterSchema_CallerOwnsTheMap(t *testing.T) {
	t.Parallel()

	// A tool definition merging these properties into a larger input, or
	// dropping the ones its endpoint ignores, edits its own copy. One shared map
	// would have the first such caller decide what every later one sees.
	first := QueryFilterSchema()
	first["type"] = "clobbered"
	delete(first, "properties")

	second := QueryFilterSchema()
	test.EqOp(t, "object", second["type"])
	test.MapNotEmpty(t, second["properties"].(map[string]any))
}

// TestQueryFilterSchema_KeysRoundTrip builds a document out of the schema's own
// property names and decodes it, which is the end of the drift this schema
// exists to stop: a key the schema names that QueryFilter does not read arrives
// as a filter silently not applied.
func TestQueryFilterSchema_KeysRoundTrip(t *testing.T) {
	t.Parallel()

	stamp := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	document := map[string]any{
		"sortBy":          *SortDescending,
		"createdAfter":    stamp,
		"createdBefore":   stamp,
		"updatedAfter":    stamp,
		"updatedBefore":   stamp,
		"maxResponseSize": MaxQueryFilterLimit,
		"includeArchived": true,
		"cursor":          "cursor_01HZY0000000000000",
	}

	// Every property the schema declares is exercised, so a field added to the
	// struct without a line here fails rather than going untested.
	test.SliceLen(t, len(document), jsonNames(t))

	for _, name := range jsonNames(t) {
		_, ok := document[name]
		test.True(t, ok, test.Sprintf("schema declares %q and this test does not send it", name))
	}

	raw, err := json.Marshal(document)
	must.NoError(t, err)

	var qf QueryFilter
	must.NoError(t, json.Unmarshal(raw, &qf))

	must.NotNil(t, qf.SortBy)
	test.EqOp(t, *SortDescending, *qf.SortBy)

	must.NotNil(t, qf.CreatedAfter)
	test.EqOp(t, stamp, *qf.CreatedAfter)

	must.NotNil(t, qf.CreatedBefore)
	test.EqOp(t, stamp, *qf.CreatedBefore)

	must.NotNil(t, qf.UpdatedAfter)
	test.EqOp(t, stamp, *qf.UpdatedAfter)

	must.NotNil(t, qf.UpdatedBefore)
	test.EqOp(t, stamp, *qf.UpdatedBefore)

	must.NotNil(t, qf.MaxResponseSize)
	test.EqOp(t, uint16(MaxQueryFilterLimit), *qf.MaxResponseSize)

	must.NotNil(t, qf.IncludeArchived)
	test.EqOp(t, true, *qf.IncludeArchived)

	must.NotNil(t, qf.Cursor)
	test.EqOp(t, "cursor_01HZY0000000000000", *qf.Cursor)
}

// TestQueryFilterSchema_MarshalsBackToItself checks the direction the round-trip
// test does not: what a QueryFilter writes is keyed the way the schema says it
// reads.
func TestQueryFilterSchema_MarshalsBackToItself(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(DefaultQueryFilter())
	must.NoError(t, err)

	var written map[string]any
	must.NoError(t, json.Unmarshal(raw, &written))

	declared := jsonNames(t)

	for name := range written {
		test.True(t, slices.Contains(declared, name), test.Sprintf("marshaled key %q is not in the schema", name))
	}
}
