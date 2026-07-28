package sqlite

import (
	"path/filepath"
	"testing"

	loggingnoop "github.com/primandproper/platform-go/v8/observability/logging/noop"
	tracingnoop "github.com/primandproper/platform-go/v8/observability/tracing/noop"

	"github.com/shoenig/test/must"
)

// BenchmarkSQLiteClient exercises a file-backed SQLite database in a temp
// directory. The driver (modernc.org/sqlite) is pure Go and needs no
// container, so this runs as part of the default `make bench`.
//
// It is deliberately not in-memory. A private ":memory:" DSN is rejected
// outright — each pooled connection would get its own database — and even a
// shared-cache one would measure a configuration nobody deploys, since
// PRAGMA journal_mode=WAL silently stays "memory" on an in-memory database.
// A file exercises the real read-pool/write-pool/WAL arrangement.
func BenchmarkSQLiteClient(b *testing.B) {
	ctx := b.Context()
	cfg := &testClientConfig{
		connectionString: filepath.Join(b.TempDir(), "bench.db"),
		maxPingAttempts:  1,
	}

	client, err := NewDatabaseClient(ctx, loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), cfg, nil)
	must.NoError(b, err)
	b.Cleanup(func() { _ = client.Close() })

	db := client.Writer()
	_, err = db.ExecContext(ctx, "CREATE TABLE bench (id INTEGER PRIMARY KEY, name TEXT)")
	must.NoError(b, err)
	_, err = db.ExecContext(ctx, "INSERT INTO bench (id, name) VALUES (1, 'seed')")
	must.NoError(b, err)

	b.Run("QueryRow", func(b *testing.B) {
		for b.Loop() {
			var name string
			_ = db.QueryRowContext(ctx, "SELECT name FROM bench WHERE id = 1").Scan(&name)
		}
	})

	b.Run("Exec", func(b *testing.B) {
		for b.Loop() {
			_, _ = db.ExecContext(ctx, "UPDATE bench SET name = 'x' WHERE id = 1")
		}
	})
}
