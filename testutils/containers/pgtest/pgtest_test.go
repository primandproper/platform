package pgtest

import (
	"context"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/testcontainers/testcontainers-go"
)

func TestNewOptions(T *testing.T) {
	T.Parallel()

	T.Run("defaults", func(t *testing.T) {
		t.Parallel()

		cfg := newOptions(nil)
		test.EqOp(t, DefaultImage, cfg.image)
		test.EqOp(t, defaultCredential, cfg.database)
		test.EqOp(t, defaultCredential, cfg.username)
		test.EqOp(t, defaultCredential, cfg.password)
		test.EqOp(t, 0, cfg.maxOpenConns)
		test.SliceEmpty(t, cfg.customizers)
	})

	T.Run("options override defaults", func(t *testing.T) {
		t.Parallel()

		cfg := newOptions([]Option{
			WithImage("pgvector/pgvector:pg17"),
			WithCredentials("vectortest", "vectoruser", "vectorpass"),
			WithMaxOpenConns(64),
			WithCustomizers(testcontainers.WithEnv(map[string]string{"FOO": "bar"})),
		})
		test.EqOp(t, "pgvector/pgvector:pg17", cfg.image)
		test.EqOp(t, "vectortest", cfg.database)
		test.EqOp(t, "vectoruser", cfg.username)
		test.EqOp(t, "vectorpass", cfg.password)
		test.EqOp(t, 64, cfg.maxOpenConns)
		test.SliceLen(t, 1, cfg.customizers)
	})

	T.Run("customizers accumulate in call order", func(t *testing.T) {
		t.Parallel()

		cfg := newOptions([]Option{
			WithCustomizers(testcontainers.WithEnv(map[string]string{"FIRST": "1"})),
			WithCustomizers(testcontainers.WithEnv(map[string]string{"SECOND": "2"})),
		})
		test.SliceLen(t, 2, cfg.customizers)
	})
}

func TestOptions_containerOptions(T *testing.T) {
	T.Parallel()

	T.Run("user customizers come last so they can override the defaults", func(t *testing.T) {
		t.Parallel()

		override := testcontainers.WithEnv(map[string]string{"FOO": "bar"})
		got := newOptions([]Option{WithCustomizers(override)}).containerOptions()

		// database, username, password, wait strategy, then the caller's own.
		test.SliceLen(t, 5, got)
	})
}

func TestRun_Container(T *testing.T) {
	T.Parallel()

	T.Run("hands the closure a queryable database", func(t *testing.T) {
		t.Parallel()

		Run(t, func(ctx context.Context, pg *Instance) {
			must.NotNil(t, pg.DB)
			must.NotNil(t, pg.Container)
			test.EqOp(t, defaultCredential, pg.Database)
			test.StrContains(t, pg.ConnectionString, "sslmode=disable")

			var current string
			must.NoError(t, pg.DB.QueryRowContext(ctx, "SELECT current_database()").Scan(&current))
			test.EqOp(t, defaultCredential, current)
		})
	})

	T.Run("honors credential overrides and reconnects under them", func(t *testing.T) {
		t.Parallel()

		Run(t, func(ctx context.Context, pg *Instance) {
			test.EqOp(t, "othertest", pg.Database)

			db := pg.Open(t, pg.ConnectionStringFor(t, pg.Database, pg.Username, pg.Password))

			var user string
			must.NoError(t, db.QueryRowContext(ctx, "SELECT current_user").Scan(&user))
			test.EqOp(t, "otheruser", user)
		}, WithCredentials("othertest", "otheruser", "otherpass"))
	})
}
