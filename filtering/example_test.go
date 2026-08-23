package filtering_test

import (
	"database/sql"
	"fmt"

	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/llm"
)

// A tool that lists something takes a page of a collection as its input, which
// is what a QueryFilter is. Handing the model this rather than a description of
// it written out beside the tool is what keeps the two from drifting: the enum,
// the bounds, and the property names are the struct's own tags.
func ExampleQueryFilterSchema() {
	tool := llm.Tool{
		Name:        "list_recipes",
		Description: "List the caller's recipes.",
		Schema:      filtering.QueryFilterSchema(),
	}

	properties, _ := tool.Schema["properties"].(map[string]any)

	// A tool whose endpoint honors only some of these drops the rest. The map
	// is this caller's own copy, so doing that affects nobody else's.
	delete(properties, "includeArchived")

	sortBy, _ := properties["sortBy"].(map[string]any)
	size, _ := properties["maxResponseSize"].(map[string]any)

	fmt.Printf("%s: %s\n", tool.Name, tool.Description)
	fmt.Println("sortBy:", sortBy["enum"])
	fmt.Println("maxResponseSize:", size["minimum"], "to", size["maximum"], "defaulting to", size["default"])

	// Output:
	// list_recipes: List the caller's recipes.
	// sortBy: [asc desc]
	// maxResponseSize: 0 to 250 defaulting to 50
}

// listRecipesParams stands in for the params struct a query generator emits.
// sqlc names these fields off the arguments in the .sql file, so a consumer's
// looks like this without platform having any say in it — which is why Bind
// hands back values to copy across rather than a type the generated struct
// would have to embed.
type listRecipesParams struct {
	CreatedAfter    sql.NullTime
	CreatedBefore   sql.NullTime
	UpdatedAfter    sql.NullTime
	UpdatedBefore   sql.NullTime
	BelongsToUser   string
	Cursor          sql.NullString
	ResultLimit     sql.NullInt32
	IncludeArchived sql.NullBool
}

// listRecipes stands in for the generated query method the params struct is
// handed to. A real one takes a context and a connection and returns rows; this
// one reports the window it was given.
func listRecipes(params *listRecipesParams) string {
	return fmt.Sprintf("owner=%s limit=%d includeArchived=%v createdAfter=%v",
		params.BelongsToUser,
		params.ResultLimit.Int32,
		params.IncludeArchived.Valid,
		params.CreatedAfter.Valid,
	)
}

// A list query binds its window from a filter, and the seven conversions that
// takes are the same seven every time. The arguments the query is keyed on are
// the caller's own — Bind does not know about them and does not touch them.
func ExampleBind() {
	filter := &filtering.QueryFilter{MaxResponseSize: new(uint16(1_000))}

	// A page size above the ceiling is answered with the ceiling rather than
	// rejected, and the clamp lands before the narrowing to the driver's type.
	// An unset field stays a NULL, which the emitted predicates coalesce to a
	// bound that admits everything.
	args := filtering.Bind(filter)

	fmt.Println(listRecipes(&listRecipesParams{
		CreatedAfter:    args.CreatedAfter,
		CreatedBefore:   args.CreatedBefore,
		UpdatedAfter:    args.UpdatedAfter,
		UpdatedBefore:   args.UpdatedBefore,
		Cursor:          args.Cursor,
		ResultLimit:     args.ResultLimit,
		IncludeArchived: args.IncludeArchived,
		BelongsToUser:   "user_001",
	}))

	// Output:
	// owner=user_001 limit=250 includeArchived=false createdAfter=false
}

// listRecipesRow stands in for the row a list query returns: the columns, plus
// the two windowed counts the same statement carried along so that the page and
// the numbers describing it come from one moment.
type listRecipesRow struct {
	ID            string
	Name          string
	FilteredCount int64
	TotalCount    int64
}

type recipe struct {
	ID   string
	Name string
}

// Turning those rows into the page an endpoint answers with is the other end of
// the same query. The conversion from a row to a domain type stays here,
// because that is the half that is genuinely about this table; the loop, the
// counts, and the cursor do not.
func ExampleDrain() {
	rows := []listRecipesRow{
		{ID: "recipe_001", Name: "gruel", FilteredCount: 2, TotalCount: 40},
		{ID: "recipe_002", Name: "porridge", FilteredCount: 2, TotalCount: 40},
	}

	page := filtering.Drain(
		rows,
		func(r listRecipesRow) *recipe { return &recipe{ID: r.ID, Name: r.Name} },
		func(r listRecipesRow) (filtered, total int64) { return r.FilteredCount, r.TotalCount },
		func(r *recipe) string { return r.ID },
		filtering.DefaultQueryFilter(),
	)

	filtered, total, known := page.Counts()

	fmt.Println("rows:", len(page.Data))
	fmt.Println("counts:", filtered, total, known)
	// The cursor reaching the next page is the last row's identifier. It is not
	// a "there is more" signal — the counts are what say that.
	fmt.Println("next cursor:", page.Cursor)

	// Output:
	// rows: 2
	// counts: 2 40 true
	// next cursor: recipe_002
}

// A decoder for a wire format reaches its page size as something wider than a
// uint16, because no wire format has one: protobuf carries a uint32, JSON hands
// a decoder a number, a query parameter hands it a string. Narrowing that to the
// field's type before the ceiling is applied wraps rather than clamps, and the
// wrapped value is indistinguishable from one the client actually sent.
// SetMaxResponseSize takes the wide value, so there is no order left to get
// wrong.
func ExampleQueryFilter_SetMaxResponseSize() {
	// What a generated protobuf message hands a converter.
	var maxResponseSize uint32 = 70000

	qf := &filtering.QueryFilter{}
	qf.SetMaxResponseSize(uint64(maxResponseSize))

	// Narrowing first would have produced 4464, which Normalize then clamps to
	// 250 — a legible-looking page size nobody asked for, raised nowhere.
	fmt.Println("clamped first:", *qf.MaxResponseSize)
	fmt.Println("narrowed first:", uint16(maxResponseSize))

	// Output:
	// clamped first: 250
	// narrowed first: 4464
}

// The page-size ceiling is a var, so a service that pages cheaply is not held
// to the number platform picked. Set it during initialization — before the
// first filter is parsed and before any schema is reflected — and the clamp and
// the document the type publishes move together.
func ExampleMaxQueryFilterLimit() {
	defer func(original uint16) { filtering.MaxQueryFilterLimit = original }(filtering.MaxQueryFilterLimit)

	filtering.MaxQueryFilterLimit = 512

	// A client asking for a thousand rows is answered with the new ceiling
	// rather than platform's.
	fmt.Println(filtering.ClampResponseSize(1000))

	// And the schema a generated client or a tool-calling model is handed says
	// so, rather than going on promising 250 while the clamp allows 512.
	properties := filtering.QueryFilterSchema()["properties"].(map[string]any)
	maxResponseSize := properties["maxResponseSize"].(map[string]any)

	fmt.Println(maxResponseSize["maximum"])

	// Output:
	// 512
	// 512
}
