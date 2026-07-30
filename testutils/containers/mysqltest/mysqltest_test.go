package mysqltest

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
		test.Eq(t, defaultParams, cfg.params)
		test.EqOp(t, 0, cfg.maxOpenConns)
		test.SliceEmpty(t, cfg.customizers)
	})

	T.Run("options override defaults", func(t *testing.T) {
		t.Parallel()

		cfg := newOptions([]Option{
			WithImage("mariadb:11"),
			WithCredentials("mariatest", "mariauser", "mariapass"),
			WithConnectionParams("parseTime=true"),
			WithMaxOpenConns(64),
			WithCustomizers(testcontainers.WithEnv(map[string]string{"FOO": "bar"})),
		})
		test.EqOp(t, "mariadb:11", cfg.image)
		test.EqOp(t, "mariatest", cfg.database)
		test.EqOp(t, "mariauser", cfg.username)
		test.EqOp(t, "mariapass", cfg.password)
		test.Eq(t, []string{"parseTime=true"}, cfg.params)
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

		Run(t, func(ctx context.Context, my *Instance) {
			must.NotNil(t, my.DB)
			must.NotNil(t, my.Container)
			test.EqOp(t, defaultCredential, my.Database)
			test.StrContains(t, my.ConnectionString, "parseTime=true")

			var current string
			must.NoError(t, my.DB.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&current))
			test.EqOp(t, defaultCredential, current)
		})
	})

	T.Run("root connection can do admin work", func(t *testing.T) {
		t.Parallel()

		Run(t, func(ctx context.Context, my *Instance) {
			root := my.Open(t, my.RootConnectionString(t, "multiStatements=true"))

			var user string
			must.NoError(t, root.QueryRowContext(ctx, "SELECT CURRENT_USER()").Scan(&user))
			test.StrHasPrefix(t, "root@", user)
		})
	})
}
