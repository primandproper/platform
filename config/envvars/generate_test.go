package envvars

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// generateFixture runs Generate over the testdata module into a temporary
// directory and returns what it wrote.
func generateFixture(t *testing.T, mutate func(*Options)) (path, contents string) {
	t.Helper()

	dir := t.TempDir()

	opts := fixtureOptions()
	opts.OutputPath = filepath.Join(dir, "envvars", "env_vars.go")

	if mutate != nil {
		mutate(opts)
	}

	must.NoError(t, Generate(t.Context(), *opts))

	raw, err := os.ReadFile(opts.outputPath())
	must.NoError(t, err)

	return opts.outputPath(), string(raw)
}

func TestGenerate(T *testing.T) {
	T.Parallel()

	T.Run("writes a file that parses as Go", func(t *testing.T) {
		t.Parallel()

		path, contents := generateFixture(t, nil)

		_, err := parser.ParseFile(token.NewFileSet(), path, contents, parser.AllErrors)
		test.NoError(t, err)
	})

	T.Run("marks the file as generated", func(t *testing.T) {
		t.Parallel()

		_, contents := generateFixture(t, nil)

		test.True(t, strings.HasPrefix(contents, generatedHeader+"\n"))
		test.StrContains(t, contents, "DO NOT EDIT.")
	})

	T.Run("declares a constant per variable, documented with what it overrides", func(t *testing.T) {
		t.Parallel()

		_, contents := generateFixture(t, nil)

		test.StrContains(t, contents, "\t// AnalyticsPosthogAPIKeyEnvVarKey is the environment variable name to set to override\n\t// `APIServiceConfig.Analytics.Posthog.APIKey`.\n\tAnalyticsPosthogAPIKeyEnvVarKey = \"APP_ANALYTICS_POSTHOG_API_KEY\"\n")
		test.StrContains(t, contents, "\tServerHTTPPortEnvVarKey = \"APP_SERVER_HTTP_PORT\"\n")
	})

	T.Run("documents a declared default", func(t *testing.T) {
		t.Parallel()

		_, contents := generateFixture(t, nil)

		test.StrContains(t, contents, "It defaults to `8000`.")
		test.StrContains(t, contents, "It defaults to `a,b,c`.")
		test.StrContains(t, contents, "It defaults to the empty string.")
	})

	T.Run("names every configuration struct a variable is reachable from", func(t *testing.T) {
		t.Parallel()

		_, contents := generateFixture(t, nil)

		test.StrContains(t, contents, "`APIServiceConfig.Database.Base.Host`, `WorkerConfig.Database.Base.Host`")
	})

	T.Run("defaults the package clause to the directory it writes into", func(t *testing.T) {
		t.Parallel()

		_, contents := generateFixture(t, nil)

		test.StrContains(t, contents, "\npackage envvars\n")
	})

	T.Run("honors an explicit package clause", func(t *testing.T) {
		t.Parallel()

		_, contents := generateFixture(t, func(o *Options) { o.Package = "envkeys" })

		test.StrContains(t, contents, "\npackage envkeys\n")
	})

	T.Run("resolves a relative output path against the module directory", func(t *testing.T) {
		t.Parallel()

		dir := writeMinimalModule(t)

		opts := Options{
			Dir:        dir,
			Prefix:     "APP_",
			UnionKey:   "internal/config.configurations",
			OutputPath: filepath.Join("internal", "config", "envvars", "env_vars.go"),
		}
		must.NoError(t, Generate(t.Context(), opts))

		contents, err := os.ReadFile(filepath.Join(dir, "internal", "config", "envvars", "env_vars.go"))
		must.NoError(t, err)
		test.StrContains(t, string(contents), `DebugEnvVarKey = "APP_DEBUG"`)
	})

	T.Run("returns an error when no output path is given", func(t *testing.T) {
		t.Parallel()

		test.Error(t, Generate(t.Context(), *fixtureOptions()))
	})

	T.Run("returns an error when the derived package name is not an identifier", func(t *testing.T) {
		t.Parallel()

		opts := fixtureOptions()
		opts.OutputPath = filepath.Join(t.TempDir(), "env-vars", "env_vars.go")

		test.Error(t, Generate(t.Context(), *opts))
	})

	T.Run("returns an error when the derived package name is a keyword", func(t *testing.T) {
		t.Parallel()

		opts := fixtureOptions()
		opts.OutputPath = filepath.Join(t.TempDir(), "range", "env_vars.go")

		test.Error(t, Generate(t.Context(), *opts))
	})

	T.Run("overwrites what it wrote last time", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "envvars", "env_vars.go")

		must.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		must.NoError(t, os.WriteFile(path, []byte("package envvars\n\n// hand-written\n"), 0o600))

		opts := fixtureOptions()
		opts.OutputPath = path
		must.NoError(t, Generate(t.Context(), *opts))

		contents, err := os.ReadFile(path)
		must.NoError(t, err)
		test.StrNotContains(t, string(contents), "hand-written")
	})
}

func TestRender(T *testing.T) {
	T.Parallel()

	T.Run("writes a compilable file for an empty variable set", func(t *testing.T) {
		t.Parallel()

		source, err := render("envvars", nil)
		must.NoError(t, err)

		test.EqOp(t, generatedHeader+"\n\npackage envvars\n", string(source))
	})

	T.Run("wraps documentation rather than running it off the page", func(t *testing.T) {
		t.Parallel()

		source, err := render("envvars", []Variable{{
			Name:         "APP_A_VERY_LONG_VARIABLE_NAME_INDEED",
			ConstantName: "AVeryLongVariableNameIndeedEnvVarKey",
			FieldPaths:   []string{"APIServiceConfig.Some.Deeply.Nested.Field", "WorkerConfig.Some.Deeply.Nested.Field"},
			Default:      "a-default",
			HasDefault:   true,
		}})
		must.NoError(t, err)

		for line := range strings.SplitSeq(string(source), "\n") {
			test.LessEq(t, commentWidth, len(strings.ReplaceAll(line, "\t", " ")))
		}
	})
}

func TestWrapComment(T *testing.T) {
	T.Parallel()

	T.Run("gives a word longer than the width a line of its own", func(t *testing.T) {
		t.Parallel()

		lines := wrapComment("short "+strings.Repeat("x", 40)+" tail", 20)

		test.Eq(t, []string{"// short", "// " + strings.Repeat("x", 40), "// tail"}, lines)
	})

	T.Run("keeps a single short line whole", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, []string{"// one two three"}, wrapComment("one two three", 40))
	})
}
