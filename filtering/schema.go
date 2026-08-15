package filtering

import (
	"encoding/json"
	"sync"

	"github.com/swaggest/jsonschema-go"
)

// queryFilterSchemaJSON is the reflected schema, marshaled once.
//
// Reflection reads nothing but QueryFilter's tags, so the document it produces
// is fixed when the package compiles and recomputing it per call would buy
// nothing. The decode is deliberately not cached, and QueryFilterSchema says
// why.
//
// InlineRefs is asked for rather than relied on. QueryFilter has no field the
// reflector would name today, but a $ref/$defs document is the one thing a tool
// definition cannot carry — a provider is handed the map unchanged and resolves
// nothing — so the option is here to keep a future nested field from turning a
// usable schema into a broken one at the far end of an API call.
var queryFilterSchemaJSON = sync.OnceValue(func() []byte {
	reflector := jsonschema.Reflector{}

	schema, err := reflector.Reflect(QueryFilter{}, jsonschema.InlineRefs)
	if err != nil {
		panic("filtering: reflecting QueryFilter's JSON Schema: " + err.Error())
	}

	raw, err := json.Marshal(schema)
	if err != nil {
		panic("filtering: marshaling QueryFilter's JSON Schema: " + err.Error())
	}

	return raw
})

// QueryFilterSchema returns the JSON Schema for QueryFilter as a decoded
// document — the shape llm.Tool.Schema takes, which is also the shape an MCP
// tool definition takes, and the same object the OpenAPI spec describes this
// type with.
//
// It is reflected off the struct rather than written out beside it, and that is
// why it lives here. A hand-written mirror of this type is a second copy that
// can be wrong: one such mirror described SortBy as the field to sort by rather
// than the direction to sort in, declared MaxResponseSize as an unbounded
// integer, and keyed on Go field names against camelCase tags. None of that was
// a mistake when it was written. The struct moved and the mirror did not, and
// nothing anywhere said so.
//
// Everything the document asserts beyond the field types is a struct tag on
// QueryFilter, so the constraints are written once for every reflector that
// reads them. The schema is therefore about the type and not about any one use
// of it: nothing here is MCP-shaped, or HTTP-shaped, and a caller wanting a
// filter described to a model and a caller generating a client both get this.
//
// The map is freshly decoded on every call and the caller owns it outright.
// Merging these properties into a larger tool input, dropping the ones an
// endpoint does not honor, or tightening a bound is editing a private copy —
// which is the point, since a shared one would have every tool definition in a
// process editing the same document.
//
// It panics if QueryFilter's tags do not reflect. That can only be a malformed
// tag in this package, which is a compile-time property of a type this package
// owns and which TestQueryFilterSchema catches before it ships; an error return
// would put an impossible branch at every call site instead, and a tool
// registry would spend its own error path on it forever.
func QueryFilterSchema() map[string]any {
	var doc map[string]any

	// These bytes came out of json.Marshal, so either this decodes or nothing
	// does.
	if err := json.Unmarshal(queryFilterSchemaJSON(), &doc); err != nil {
		panic("filtering: decoding QueryFilter's JSON Schema: " + err.Error())
	}

	return doc
}
