package querygen_test

import (
	"fmt"
	"strings"

	"github.com/primandproper/platform-go/v11/database/querygen"
)

// A table's generator names the table and its columns, and the standard set
// follows from them. What a consumer writes per table is the schema; what this
// package writes is the conventions.
func ExampleStandardCRUD() {
	queries := querygen.StandardCRUD("webhooks", []string{
		querygen.IDColumn,
		"name",
		"url",
		querygen.BelongsToAccountColumn,
		querygen.CreatedAtColumn,
		querygen.LastUpdatedAtColumn,
		querygen.ArchivedAtColumn,
	},
		querygen.WithEntity("Webhook", "Webhooks"),
		querygen.WithOwnership(querygen.BelongsToAccountColumn),
	)

	for _, query := range queries {
		fmt.Printf("%s %s\n", query.Annotation.Name, query.Annotation.Type)
	}

	// Output:
	// CreateWebhook :exec
	// GetWebhook :one
	// CheckWebhookExistence :one
	// ListWebhooks :many
	// UpdateWebhook :execrows
	// ArchiveWebhook :execrows
}

// RenderFile produces the bytes a .sql file holds, which is what a generator
// writes and what its --check mode compares against.
func ExampleRenderFile() {
	queries := querygen.StandardCRUD("things", []string{querygen.IDColumn, querygen.ArchivedAtColumn},
		querygen.WithEntity("Thing", "Things"))

	file := querygen.RenderFile(queries)

	// Just the annotations, to keep the example short.
	for line := range strings.SplitSeq(file, "\n") {
		if strings.HasPrefix(line, "-- name:") {
			fmt.Println(line)
		}
	}

	// Output:
	// -- name: CreateThing :exec
	// -- name: GetThing :one
	// -- name: CheckThingExistence :one
	// -- name: ListThings :many
	// -- name: ArchiveThing :execrows
}

// The fragment builders are there for the queries a table needs beyond the
// standard set — a search, a scoped list — so that those agree with the standard
// ones about what a filter means.
func ExampleFilterConditions() {
	columns := []string{querygen.IDColumn, querygen.CreatedAtColumn, querygen.ArchivedAtColumn}

	fmt.Println(querygen.FilterConditions("things", columns, "things.name "+querygen.ILIKECondition("name_query")))

	// Output:
	// things.created_at > COALESCE(sqlc.narg(created_after), (SELECT NOW() - '999 years'::INTERVAL))
	// 	AND things.created_at < COALESCE(sqlc.narg(created_before), (SELECT NOW() + '999 years'::INTERVAL))
	// 	AND (COALESCE(sqlc.narg(include_archived), false)::boolean OR things.archived_at IS NULL)
	// 	AND things.name ILIKE '%' || sqlc.arg(name_query)::text || '%'
	// 	AND things.id > COALESCE(sqlc.narg(cursor), '')
}
