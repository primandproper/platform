package envvars

import (
	goast "go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// minimalModuleConfig is the smallest thing this package can generate from: one
// constraint with one member, and one variable to find on it.
const minimalModuleConfig = `package config

type configurations interface {
	ServiceConfig
}

type ServiceConfig struct {
	Debug bool ` + "`env:\"DEBUG\"`" + `
}
`

// writeMinimalModule writes that module into a temporary directory and returns
// its root.
func writeMinimalModule(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	must.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/minimal\n\ngo 1.26\n"), 0o600))
	must.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "config"), 0o750))
	must.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "config", "config.go"), []byte(minimalModuleConfig), 0o600))

	return dir
}

// parseFixtureFile parses source as a file of the package under test.
func parseFixtureFile(t *testing.T, source string) *goast.File {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", source, parser.SkipObjectResolution)
	must.NoError(t, err)

	return file
}

func TestIndexParseModule(T *testing.T) {
	T.Parallel()

	T.Run("keys this module's packages on their module-relative directory", func(t *testing.T) {
		t.Parallel()

		idx := newIndex()
		must.NoError(t, idx.parseModule(writeMinimalModule(t), "", "example.com/minimal"))

		test.MapContainsKey(t, idx.structs, "internal/config.ServiceConfig")
		test.MapContainsKey(t, idx.unions, "internal/config.configurations")
		test.Eq(t, []string{"internal/config.ServiceConfig"}, idx.unions["internal/config.configurations"])
	})

	T.Run("keys a dependency's packages on their import path", func(t *testing.T) {
		t.Parallel()

		idx := newIndex()
		must.NoError(t, idx.parseModule(filepath.Join("testdata", "dep"), "example.com/dep", "example.com/app"))

		test.MapContainsKey(t, idx.structs, "example.com/dep/database.Config")
		test.MapContainsKey(t, idx.structs, "example.com/dep/observability.Config")
	})

	T.Run("skips a nested module, whose types are not this module's", func(t *testing.T) {
		t.Parallel()

		dir := writeMinimalModule(t)
		nested := filepath.Join(dir, "tools")
		must.NoError(t, os.MkdirAll(nested, 0o750))
		must.NoError(t, os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module example.com/tools\n\ngo 1.26\n"), 0o600))
		must.NoError(t, os.WriteFile(filepath.Join(nested, "tools.go"), []byte("package tools\n\ntype Config struct{}\n"), 0o600))

		idx := newIndex()
		must.NoError(t, idx.parseModule(dir, "", "example.com/minimal"))

		test.MapNotContainsKey(t, idx.structs, "tools.Config")
	})

	T.Run("skips vendored, hidden, and fixture directories", func(t *testing.T) {
		t.Parallel()

		dir := writeMinimalModule(t)

		for _, name := range []string{"vendor", "testdata", ".hidden", "_ignored"} {
			sub := filepath.Join(dir, name)
			must.NoError(t, os.MkdirAll(sub, 0o750))
			must.NoError(t, os.WriteFile(filepath.Join(sub, "x.go"), []byte("package x\n\ntype Config struct{}\n"), 0o600))
		}

		idx := newIndex()
		must.NoError(t, idx.parseModule(dir, "", "example.com/minimal"))

		for _, name := range []string{"vendor", "testdata", ".hidden", "_ignored"} {
			test.MapNotContainsKey(t, idx.structs, name+".Config")
		}
	})

	T.Run("skips test files, whose types are not loadable", func(t *testing.T) {
		t.Parallel()

		dir := writeMinimalModule(t)
		must.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "config", "helper_test.go"), []byte("package config\n\ntype TestOnlyConfig struct{}\n"), 0o600))

		idx := newIndex()
		must.NoError(t, idx.parseModule(dir, "", "example.com/minimal"))

		test.MapNotContainsKey(t, idx.structs, "internal/config.TestOnlyConfig")
	})

	T.Run("reports a file it cannot parse rather than dropping what it declares", func(t *testing.T) {
		t.Parallel()

		dir := writeMinimalModule(t)
		must.NoError(t, os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package !!!\n"), 0o600))

		test.Error(t, newIndex().parseModule(dir, "", "example.com/minimal"))
	})

	T.Run("reports a directory that does not exist", func(t *testing.T) {
		t.Parallel()

		test.Error(t, newIndex().parseModule(filepath.Join(t.TempDir(), "absent"), "", "example.com/minimal"))
	})
}

func TestPackageImports(T *testing.T) {
	T.Parallel()

	T.Run("resolves each import to the key its package is indexed under", func(t *testing.T) {
		t.Parallel()

		file := parseFixtureFile(t, `package config

import (
	"strings"

	"example.com/app"
	"example.com/app/internal/database"
	renamed "example.com/dep/observability"
)
`)

		test.Eq(t, map[string]string{
			"strings":  "strings",
			"app":      ".",
			"database": "internal/database",
			"renamed":  "example.com/dep/observability",
		}, packageImports(file, "example.com/app"))
	})
}

func TestUnionMembers(T *testing.T) {
	T.Parallel()

	unionFrom := func(t *testing.T, source string) []string {
		t.Helper()

		file := parseFixtureFile(t, source)
		idx := newIndex()
		idx.addFile(file, "internal/config", "example.com/app")

		return idx.unions["internal/config.constraint"]
	}

	T.Run("resolves same-package members", func(t *testing.T) {
		t.Parallel()

		members := unionFrom(t, "package config\n\ntype constraint interface {\n\tA | B | C\n}\n")

		test.Eq(t, []string{"internal/config.A", "internal/config.B", "internal/config.C"}, members)
	})

	T.Run("resolves members from another package", func(t *testing.T) {
		t.Parallel()

		members := unionFrom(t, "package config\n\nimport \"example.com/dep/database\"\n\ntype constraint interface {\n\tA | database.Config\n}\n")

		test.Eq(t, []string{"internal/config.A", "example.com/dep/database.Config"}, members)
	})

	T.Run("resolves approximate terms", func(t *testing.T) {
		t.Parallel()

		members := unionFrom(t, "package config\n\ntype constraint interface {\n\t~A | ~B\n}\n")

		test.Eq(t, []string{"internal/config.A", "internal/config.B"}, members)
	})

	T.Run("ignores an interface with methods", func(t *testing.T) {
		t.Parallel()

		test.SliceEmpty(t, unionFrom(t, "package config\n\ntype constraint interface {\n\tDo() error\n}\n"))
	})

	T.Run("ignores a union of unnamed types", func(t *testing.T) {
		t.Parallel()

		test.SliceEmpty(t, unionFrom(t, "package config\n\ntype constraint interface {\n\t[]byte | map[string]int\n}\n"))
	})
}

func TestPackageKey(T *testing.T) {
	T.Parallel()

	T.Run("keys packages of this module on their directory", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, ".", packageKey("", "."))
		test.EqOp(t, "internal/config", packageKey("", filepath.Join("internal", "config")))
	})

	T.Run("keys packages of a dependency on their import path", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "example.com/dep", packageKey("example.com/dep", "."))
		test.EqOp(t, "example.com/dep/database", packageKey("example.com/dep", "database"))
	})
}
