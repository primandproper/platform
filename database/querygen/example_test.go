package querygen_test

import (
	"fmt"
	"strings"

	"github.com/primandproper/platform-go/v14/database/dialect"
	"github.com/primandproper/platform-go/v14/database/querygen"
)

// A table's generator names the dialect, the table and its columns, and the
// standard set follows from them. What a consumer writes per table is the
// schema; what this package writes is the conventions.
func ExampleGenerator_StandardCRUD() {
	queries := querygen.For(dialect.Postgres).StandardCRUD("webhooks", []string{
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
	// ListWebhooksDescending :many
	// UpdateWebhook :execrows
	// ArchiveWebhook :execrows
}

// The dialect decides the SQL and not the shape: the same table yields the same
// query names with the same arguments on all three, so the application code over
// the generated methods is written once.
func ExampleFor() {
	for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
		for line := range strings.SplitSeq(querygen.For(d).ReindexScanQuery("things"), "\n") {
			if strings.HasPrefix(line, "ORDER BY") {
				fmt.Printf("%s: %s\n", d, line)
			}
		}
	}

	// Output:
	// postgres: ORDER BY things.id COLLATE "C"
	// mysql: ORDER BY CAST(things.id AS BINARY)
	// sqlite: ORDER BY things.id COLLATE BINARY
}

// RenderFile produces the bytes a .sql file holds, which is what a generator
// writes and what its --check mode compares against.
func ExampleRenderFile() {
	queries := querygen.For(dialect.SQLite).StandardCRUD("things",
		[]string{querygen.IDColumn, querygen.ArchivedAtColumn},
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
	// -- name: ListThingsDescending :many
	// -- name: ArchiveThing :execrows
}

// The fragment builders are there for the queries a table needs beyond the
// standard set — a search, a scoped list — so that those agree with the standard
// ones about what a filter means, and speak the same dialect while they do it.
func ExampleGenerator_FilterConditions() {
	g := querygen.For(dialect.Postgres)
	columns := []string{querygen.IDColumn, querygen.CreatedAtColumn, querygen.ArchivedAtColumn}

	fmt.Println(g.FilterConditions("things", columns, querygen.Ascending, g.ContainsCondition("things.name", "name_query")))

	// Output:
	// things.created_at > COALESCE(sqlc.narg(created_after), (SELECT CURRENT_TIMESTAMP - '999 years'::INTERVAL))
	// 	AND things.created_at < COALESCE(sqlc.narg(created_before), (SELECT CURRENT_TIMESTAMP + '999 years'::INTERVAL))
	// 	AND (COALESCE(sqlc.narg(include_archived), false)::boolean OR things.archived_at IS NULL)
	// 	AND things.name ILIKE '%' || sqlc.arg(name_query)::text || '%'
	// 	AND things.id > COALESCE(sqlc.narg(page_cursor), '')
}

// The registry is the list of tables an application has, which is not the same
// list as the tables something generates SQL for. StandardCRUD feeds it, and a
// table whose SQL is written by hand — or by something else entirely — feeds the
// same list, so whoever truncates them between integration tests reads one list
// rather than remembering there are two.
//
// The package-level RegisterTable and RegisteredTables are the usual pair; this
// example uses an explicit registry so its output does not depend on what else
// in the process has registered.
func ExampleRegistry() {
	registry := querygen.NewRegistry()

	querygen.For(dialect.Postgres).StandardCRUD("webhooks",
		[]string{querygen.IDColumn, "url", querygen.ArchivedAtColumn},
		querygen.WithEntity("Webhook", "Webhooks"),
		querygen.WithRegistry(registry),
	)

	// No queries come from here, and the rows still have to go somewhere.
	registry.Register("sessions", "webauthn_credentials")

	fmt.Println(strings.Join(registry.Tables(), ", "))

	// Output:
	// sessions, webauthn_credentials, webhooks
}

// A junction list is the one read here that spans two tables. What decides which
// of them is listed is what a page is a page of: a roster is a page of
// memberships with the member attached, so memberships is listed, the cursor
// walks its id, and the user's columns arrive beside them under a prefix.
func ExampleGenerator_JunctionListQueries() {
	roster := querygen.For(dialect.Postgres).JunctionListQueries(
		"ListAccountMembers", "memberships",
		[]string{querygen.IDColumn, querygen.BelongsToAccountColumn, "belongs_to_user", querygen.ArchivedAtColumn},
		&querygen.Junction{
			Table:    "users",
			Column:   querygen.IDColumn,
			OnColumn: "belongs_to_user",
			Columns:  []string{querygen.IDColumn, "username", querygen.ArchivedAtColumn},
			Prefix:   "user",
		},
		querygen.Match{Column: querygen.BelongsToAccountColumn},
	)

	// Two statements come back rather than one: a page has a direction, and a
	// direction is statement text rather than a bound value. The ORDER BY is
	// where they part company.
	for _, query := range roster {
		fmt.Println(query.Annotation.Name)

		for line := range strings.SplitSeq(query.Content, "\n") {
			if strings.HasPrefix(line, "FROM") || strings.HasPrefix(line, "JOIN") ||
				strings.HasPrefix(line, "ORDER BY") || strings.Contains(line, " AS user_") {
				fmt.Println(strings.TrimSpace(line))
			}
		}
	}

	// Output:
	// ListAccountMembers
	// users.id AS user_id,
	// users.username AS user_username,
	// users.archived_at AS user_archived_at,
	// FROM memberships
	// JOIN users ON memberships.belongs_to_user=users.id
	// ORDER BY memberships.id ASC
	// ListAccountMembersDescending
	// users.id AS user_id,
	// users.username AS user_username,
	// users.archived_at AS user_archived_at,
	// FROM memberships
	// JOIN users ON memberships.belongs_to_user=users.id
	// ORDER BY memberships.id DESC
}

// The bounded prune is the one shape whose three renderings are three
// statements. The call is the same on every dialect — one name, one key, one
// horizon, one cap — and what differs is where the grammar will accept the
// bound.
func ExampleGenerator_PruneQuery() {
	prune := querygen.Prune{
		Key:   []string{"idempotency_key"},
		Order: []querygen.Order{{Column: "recorded_at"}},
	}
	horizon := querygen.Match{Column: "recorded_at", Arg: "horizon", Against: querygen.AtMostArgument}

	for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL} {
		query := querygen.For(d).PruneQuery("PruneMeteringEvents", "metering_events", prune, horizon)

		fmt.Printf("-- %s\n%s\n", d, query.Content)
	}

	// Output:
	// -- postgres
	// DELETE FROM metering_events
	// WHERE idempotency_key IN (
	//	SELECT doomed.idempotency_key
	//	FROM metering_events AS doomed
	//	WHERE doomed.recorded_at <= sqlc.arg(horizon)
	//	ORDER BY doomed.recorded_at ASC
	//	LIMIT sqlc.arg(result_limit)
	//	FOR UPDATE SKIP LOCKED
	// );
	// -- mysql
	// DELETE FROM metering_events
	// WHERE recorded_at <= sqlc.arg(horizon)
	// ORDER BY recorded_at ASC
	// LIMIT ?;
}
