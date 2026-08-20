package resources_test

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v12/database"
	"github.com/primandproper/platform-go/v12/database/dialect"
	"github.com/primandproper/platform-go/v12/database/sqlite"
	"github.com/primandproper/platform-go/v12/resources"

	"github.com/shoenig/test/must"
)

// testClientConfig is the minimum database.ClientConfig a client needs.
type testClientConfig struct {
	connectionString string
}

var _ database.ClientConfig = (*testClientConfig)(nil)

func (c *testClientConfig) GetReadConnectionString() string   { return c.connectionString }
func (c *testClientConfig) GetWriteConnectionString() string  { return c.connectionString }
func (c *testClientConfig) GetMaxPingAttempts() uint64        { return 1 }
func (c *testClientConfig) GetPingWaitPeriod() time.Duration  { return time.Millisecond }
func (c *testClientConfig) GetMaxIdleConns() int              { return 2 }
func (c *testClientConfig) GetMaxOpenConns() int              { return 1 }
func (c *testClientConfig) GetConnMaxLifetime() time.Duration { return time.Minute }

// Comment is dinnerdonebetter's internal/domain/comments.Comment, verbatim.
// Nothing about it was changed to suit this package, which is the point.
type Comment struct {
	_ struct{} `json:"-"`

	CreatedAt       time.Time  `json:"createdAt"`
	ParentCommentID *string    `json:"parentCommentId,omitempty"`
	LastUpdatedAt   *time.Time `json:"lastUpdatedAt,omitempty"`
	ArchivedAt      *time.Time `json:"archivedAt,omitempty"`
	ID              string     `json:"id"`
	Content         string     `json:"content"`
	TargetType      string     `json:"targetType"`
	ReferencedID    string     `json:"referencedId"`
	BelongsToUser   string     `json:"belongsToUser"`
}

// commentsDefinition is the whole of dinnerdonebetter's comments repository,
// expressed as a declaration.
//
// Compare against internal/repositories/postgres/comments: 409 lines of Go, 386
// of generated sqlc, and a 141-line query generator. What replaces them is this,
// and it is a list of the columns the migration already declares.
func commentsDefinition() resources.Definition[Comment] {
	return resources.Definition[Comment]{
		Name:  "comment",
		Table: "comments",

		Columns: []resources.Column[Comment]{
			resources.ID(func(c *Comment) *string { return &c.ID }),
			resources.Field("content", func(c *Comment) *string { return &c.Content }),
			resources.Field("target_type", func(c *Comment) *string { return &c.TargetType }).Immutable(),
			resources.Field("referenced_id", func(c *Comment) *string { return &c.ReferencedID }).Immutable(),
			resources.Field("parent_comment_id", func(c *Comment) **string { return &c.ParentCommentID }).Immutable(),
			// belongs_to_user is authorship, not tenancy: anyone who can see a
			// recipe sees every author's comments on it, and only the author may
			// edit theirs. OwnerWrites is that sentence.
			resources.Owner("belongs_to_user", func(c *Comment) *string { return &c.BelongsToUser }, resources.OwnerWrites),
			resources.Field("created_at", func(c *Comment) *time.Time { return &c.CreatedAt }),
			resources.Field("last_updated_at", func(c *Comment) **time.Time { return &c.LastUpdatedAt }),
			resources.Field("archived_at", func(c *Comment) **time.Time { return &c.ArchivedAt }),
		},

		// The comments table has no account column. Every row is global, and
		// saying so is a declaration rather than an omission — the methods still
		// take a scope, so the day comments gain an account this line changes
		// and no call site does.
		Scoping: resources.Unscoped,

		Lookups: []resources.Lookup{
			// GetCommentsForReference, and the index behind it.
			resources.On("target_type", "referenced_id"),
			// GetCommentsForUser, which the data-privacy collector reads through.
			resources.On("belongs_to_user"),
		},
	}
}

// commentsSQLiteSchema is the comments table as SQLite spells it: the same
// columns, with the Postgres enum as the text it is on every other engine.
const commentsSQLiteSchema = `
CREATE TABLE IF NOT EXISTS comments (
    id TEXT NOT NULL PRIMARY KEY,
    content TEXT NOT NULL DEFAULT '',
    target_type TEXT NOT NULL,
    referenced_id TEXT NOT NULL,
    parent_comment_id TEXT REFERENCES comments(id) ON DELETE CASCADE,
    belongs_to_user TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at DATETIME,
    archived_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_comments_reference ON comments (target_type, referenced_id) WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_comments_user ON comments (belongs_to_user) WHERE archived_at IS NULL;
`

// newSQLiteClient builds a client over a fresh database file.
//
// SQLite is where the behavioral suite runs, because it is a real server that
// parses the real statements without a container: the placeholder rendering, the
// cursor walk, the nullable round trip and the owner gate are all exercised
// against something that would reject them. What it cannot exercise is the
// Postgres array binding a set read uses and a column whose type is an enum,
// which is what containers_test.go is for.
func newSQLiteClient(t *testing.T) database.Client {
	t.Helper()

	client, err := sqlite.NewDatabaseClient(t.Context(),
		&testClientConfig{connectionString: filepath.Join(t.TempDir(), "resources.db")})
	must.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	mustExec(t, client, commentsSQLiteSchema)

	return client
}

// newCommentStore builds a SQLite-backed store over the comments declaration,
// along with the changes its hook saw.
func newCommentStore(t *testing.T, opts ...resources.Option) (*resources.Store[Comment], *changeLog) {
	t.Helper()

	client := newSQLiteClient(t)

	resource, err := resources.Define(dialect.SQLite, commentsDefinition())
	must.NoError(t, err)

	seen := &changeLog{}

	store, err := resources.NewStore(resource, client,
		append([]resources.Option{resources.WithHook(seen.record)}, opts...)...)
	must.NoError(t, err)

	return store, seen
}

// changeLog collects what the hooks were told, so a test can assert on the
// account a write gave of itself.
//
// It is guarded because a store's hook runs wherever the write does, and the
// container suite's subtests write in parallel.
type changeLog struct {
	changes []resources.Change[Comment]
	mu      sync.Mutex
}

func (l *changeLog) record(_ context.Context, _ database.SQLQueryExecutor, change resources.Change[Comment]) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.changes = append(l.changes, change)

	return nil
}

// mark returns the position a later since is relative to.
func (l *changeLog) mark() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.changes)
}

// since returns the changes recorded after a mark.
func (l *changeLog) since(mark int) []resources.Change[Comment] {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.changes[mark:])
}

// mustExec applies raw DDL through the client's writer.
func mustExec(tb testing.TB, client database.Client, statements string) {
	tb.Helper()

	for _, statement := range dialect.SplitStatements(statements) {
		_, err := client.Writer().ExecContext(tb.Context(), statement)
		must.NoError(tb, err)
	}
}
