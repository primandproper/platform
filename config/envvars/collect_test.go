package envvars

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// fixtureOptions points at the testdata module pair, which stands in for a
// service assembled out of a library's configuration structs.
func fixtureOptions() *Options {
	return &Options{
		Dir:      filepath.Join("testdata", "app"),
		Prefix:   "APP_",
		UnionKey: "internal/config.configurations",
		DependencyDirs: map[string]string{
			"example.com/dep": filepath.Join("testdata", "dep"),
		},
	}
}

// collectFixture collects from the fixture module and indexes the result by
// variable name.
func collectFixture(t *testing.T, opts *Options) map[string]Variable {
	t.Helper()

	variables, err := Collect(t.Context(), *opts)
	must.NoError(t, err)

	byName := make(map[string]Variable, len(variables))
	for i := range variables {
		byName[variables[i].Name] = variables[i]
	}

	return byName
}

func TestCollect(T *testing.T) {
	T.Parallel()

	T.Run("derives every variable reachable from the union's members", func(t *testing.T) {
		t.Parallel()

		variables, err := Collect(t.Context(), *fixtureOptions())
		must.NoError(t, err)

		names := make([]string, 0, len(variables))
		for i := range variables {
			names = append(names, variables[i].Name)
		}

		test.Eq(t, []string{
			"APP_ANALYTICS_POSTHOG_API_KEY",
			"APP_CHAIN_LABEL",
			"APP_DATABASE_CONNECTION_PORT",
			"APP_DATABASE_DEBUG",
			"APP_DATABASE_HOST",
			"APP_DEBUG",
			"APP_INLINE_TOKEN",
			"APP_NAME",
			"APP_OBSERVABILITY_AUDIT_PORT",
			"APP_OBSERVABILITY_LOGGING_LEVEL",
			"APP_QUEUE_NAME",
			"APP_REGION",
			"APP_SERVER_HTTP_PORT",
		}, names)
	})

	T.Run("resolves a struct from a dependency module and the packages it imports", func(t *testing.T) {
		t.Parallel()

		byName := collectFixture(t, fixtureOptions())

		test.Eq(t, []string{"APIServiceConfig.Database.ConnectionDetails.Port", "WorkerConfig.Database.ConnectionDetails.Port"},
			byName["APP_DATABASE_CONNECTION_PORT"].FieldPaths)
		test.Eq(t, []string{"APIServiceConfig.Observability.Audit.Port"},
			byName["APP_OBSERVABILITY_AUDIT_PORT"].FieldPaths)
	})

	T.Run("promotes an embedded struct's fields under the outer prefix", func(t *testing.T) {
		t.Parallel()

		host := collectFixture(t, fixtureOptions())["APP_DATABASE_HOST"]

		test.Eq(t, []string{"APIServiceConfig.Database.Base.Host", "WorkerConfig.Database.Base.Host"}, host.FieldPaths)
		test.EqOp(t, "localhost", host.Default)
		test.True(t, host.HasDefault)
	})

	T.Run("walks a struct field that declares no prefix", func(t *testing.T) {
		t.Parallel()

		region := collectFixture(t, fixtureOptions())["APP_REGION"]

		test.Eq(t, []string{"APIServiceConfig.Unprefixed.Region"}, region.FieldPaths)
	})

	T.Run("distinguishes an empty default from no default", func(t *testing.T) {
		t.Parallel()

		byName := collectFixture(t, fixtureOptions())

		test.True(t, byName["APP_REGION"].HasDefault)
		test.EqOp(t, "", byName["APP_REGION"].Default)

		test.False(t, byName["APP_NAME"].HasDefault)
		test.EqOp(t, "", byName["APP_NAME"].Default)
	})

	T.Run("keeps a default that contains a comma whole", func(t *testing.T) {
		t.Parallel()

		queue := collectFixture(t, fixtureOptions())["APP_QUEUE_NAME"]

		test.EqOp(t, "a,b,c", queue.Default)
	})

	T.Run("walks an inline struct and a struct behind a pointer", func(t *testing.T) {
		t.Parallel()

		byName := collectFixture(t, fixtureOptions())

		test.Eq(t, []string{"APIServiceConfig.Inline.Token"}, byName["APP_INLINE_TOKEN"].FieldPaths)
		test.Eq(t, []string{"APIServiceConfig.Server.HTTPPort"}, byName["APP_SERVER_HTTP_PORT"].FieldPaths)
		test.EqOp(t, "8000", byName["APP_SERVER_HTTP_PORT"].Default)
	})

	T.Run("stops at the first repetition of a self-referential struct", func(t *testing.T) {
		t.Parallel()

		byName := collectFixture(t, fixtureOptions())

		test.Eq(t, []string{"APIServiceConfig.Chain.Label"}, byName["APP_CHAIN_LABEL"].FieldPaths)

		_, deeper := byName["APP_CHAIN_NEXT_LABEL"]
		test.False(t, deeper)
	})

	T.Run("omits what the parser will not read", func(t *testing.T) {
		t.Parallel()

		byName := collectFixture(t, fixtureOptions())

		for _, absent := range []string{
			"APP_SECRET",      // unexported field
			"APP_-",           // env:"-"
			"APP_TENANT_SLUG", // struct behind a slice
			"APP_TENANTSLUG",  // ditto, whatever the indexing looks like
		} {
			_, found := byName[absent]
			test.False(t, found, test.Sprintf("expected %q not to be derived", absent))
		}
	})

	T.Run("records one field path per configuration struct a variable is reachable from", func(t *testing.T) {
		t.Parallel()

		debug := collectFixture(t, fixtureOptions())["APP_DATABASE_DEBUG"]

		test.Eq(t, []string{"APIServiceConfig.Database.Debug", "WorkerConfig.Database.Debug"}, debug.FieldPaths)
	})

	T.Run("names constants through common initialisms", func(t *testing.T) {
		t.Parallel()

		byName := collectFixture(t, fixtureOptions())

		test.EqOp(t, "AnalyticsPosthogAPIKeyEnvVarKey", byName["APP_ANALYTICS_POSTHOG_API_KEY"].ConstantName)
		test.EqOp(t, "ServerHTTPPortEnvVarKey", byName["APP_SERVER_HTTP_PORT"].ConstantName)
		test.EqOp(t, "DebugEnvVarKey", byName["APP_DEBUG"].ConstantName)
	})

	T.Run("leaves the prefix out of the constant name", func(t *testing.T) {
		t.Parallel()

		opts := fixtureOptions()
		opts.Prefix = ""

		unprefixed := collectFixture(t, opts)
		prefixed := collectFixture(t, fixtureOptions())

		test.EqOp(t, unprefixed["DEBUG"].ConstantName, prefixed["APP_DEBUG"].ConstantName)
		test.EqOp(t, "DEBUG", unprefixed["DEBUG"].Name)
	})

	T.Run("walks only the roots it is given", func(t *testing.T) {
		t.Parallel()

		opts := fixtureOptions()
		opts.UnionKey = ""
		opts.Roots = []string{"internal/config.WorkerConfig"}

		byName := collectFixture(t, opts)

		test.MapContainsKey(t, byName, "APP_QUEUE_NAME")
		test.MapNotContainsKey(t, byName, "APP_DEBUG")
		test.Eq(t, []string{"WorkerConfig.Database.Base.Host"}, byName["APP_DATABASE_HOST"].FieldPaths)
	})

	T.Run("returns an error when neither roots nor a union are named", func(t *testing.T) {
		t.Parallel()

		opts := fixtureOptions()
		opts.UnionKey = ""

		_, err := Collect(t.Context(), *opts)

		test.Error(t, err)
	})

	T.Run("returns an error when both roots and a union are named", func(t *testing.T) {
		t.Parallel()

		opts := fixtureOptions()
		opts.Roots = []string{"internal/config.WorkerConfig"}

		_, err := Collect(t.Context(), *opts)

		test.Error(t, err)
	})

	T.Run("returns an error when the union does not exist", func(t *testing.T) {
		t.Parallel()

		opts := fixtureOptions()
		opts.UnionKey = "internal/config.notAConstraint"

		_, err := Collect(t.Context(), *opts)

		test.Error(t, err)
	})

	T.Run("returns an error when a named root does not exist", func(t *testing.T) {
		t.Parallel()

		opts := fixtureOptions()
		opts.UnionKey = ""
		opts.Roots = []string{"internal/config.NoSuchConfig"}

		_, err := Collect(t.Context(), *opts)

		test.Error(t, err)
	})

	T.Run("returns an error when the directory is not a module", func(t *testing.T) {
		t.Parallel()

		opts := fixtureOptions()
		opts.Dir = t.TempDir()

		_, err := Collect(t.Context(), *opts)

		test.Error(t, err)
	})

	T.Run("loses a dependency's variables when its source is not given", func(t *testing.T) {
		t.Parallel()

		opts := fixtureOptions()
		opts.DependencyDirs = nil

		byName := collectFixture(t, opts)

		test.MapContainsKey(t, byName, "APP_DEBUG")
		test.MapNotContainsKey(t, byName, "APP_DATABASE_HOST")
	})
}

func TestWalkerVariables(T *testing.T) {
	T.Parallel()

	T.Run("reports two variables that would share one constant", func(t *testing.T) {
		t.Parallel()

		w := &walker{vars: map[string]*variable{
			"FOO_BAR":  {fieldPaths: []string{"ServiceConfig.Foo"}},
			"FOO__BAR": {fieldPaths: []string{"ServiceConfig.Bar"}},
		}}

		_, err := w.variables("APP_")

		test.Error(t, err)
		test.ErrorContains(t, err, "FooBarEnvVarKey")
	})

	T.Run("reports a variable that yields no usable identifier", func(t *testing.T) {
		t.Parallel()

		w := &walker{vars: map[string]*variable{"1_FOO": {fieldPaths: []string{"ServiceConfig.Foo"}}}}

		_, err := w.variables("APP_")

		test.Error(t, err)
	})
}

func TestCollect_ordering(T *testing.T) {
	T.Parallel()

	T.Run("returns the same answer every time", func(t *testing.T) {
		t.Parallel()

		first, err := Collect(t.Context(), *fixtureOptions())
		must.NoError(t, err)

		for range 3 {
			again, collectErr := Collect(context.Background(), *fixtureOptions())
			must.NoError(t, collectErr)
			test.Eq(t, first, again)
		}

		test.True(t, slices.IsSortedFunc(first, func(a, b Variable) int {
			switch {
			case a.Name < b.Name:
				return -1
			case a.Name > b.Name:
				return 1
			default:
				return 0
			}
		}))
	})
}
