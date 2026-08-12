package envvars

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// writeVendorTree writes a vendor directory holding the given modules, and
// returns the module root it belongs to. A module named here without a
// directory of its own stands for one whose packages were not vendored.
func writeVendorTree(t *testing.T, listed, present []string) string {
	t.Helper()

	dir := t.TempDir()
	vendorDir := filepath.Join(dir, "vendor")
	must.NoError(t, os.MkdirAll(vendorDir, 0o750))

	var modulesTxt strings.Builder
	for _, module := range listed {
		modulesTxt.WriteString("# " + module + " v1.2.3\n## explicit; go 1.26\n" + module + "/pkg\n")
	}

	must.NoError(t, os.WriteFile(filepath.Join(vendorDir, "modules.txt"), []byte(modulesTxt.String()), 0o600))

	for _, module := range present {
		must.NoError(t, os.MkdirAll(filepath.Join(vendorDir, filepath.FromSlash(module)), 0o750))
	}

	return dir
}

func TestOptionsResolved(T *testing.T) {
	T.Parallel()

	T.Run("defaults the directory to the working directory", func(t *testing.T) {
		t.Parallel()

		opts := Options{UnionKey: "internal/config.configurations"}

		must.NoError(t, opts.applyDefaults())
		test.EqOp(t, ".", opts.Dir)
	})

	T.Run("requires one of a union or roots", func(t *testing.T) {
		t.Parallel()

		opts := Options{Dir: "."}

		test.Error(t, opts.applyDefaults())
	})

	T.Run("refuses both a union and roots", func(t *testing.T) {
		t.Parallel()

		opts := Options{UnionKey: "internal/config.configurations", Roots: []string{"internal/config.APIServiceConfig"}}

		test.Error(t, opts.applyDefaults())
	})
}

func TestOptionsResolvedForGeneration(T *testing.T) {
	T.Parallel()

	T.Run("derives the package name from the output directory", func(t *testing.T) {
		t.Parallel()

		opts := Options{
			UnionKey:   "internal/config.configurations",
			OutputPath: filepath.Join("internal", "config", "envvars", "env_vars.go"),
		}

		must.NoError(t, opts.applyGenerationDefaults())
		test.EqOp(t, "envvars", opts.Package)
	})

	T.Run("requires an output path", func(t *testing.T) {
		t.Parallel()

		opts := Options{UnionKey: "internal/config.configurations"}

		test.Error(t, opts.applyGenerationDefaults())
	})

	T.Run("refuses a derived package name that is not an identifier", func(t *testing.T) {
		t.Parallel()

		opts := Options{
			UnionKey:   "internal/config.configurations",
			OutputPath: filepath.Join("env-vars", "env_vars.go"),
		}

		test.Error(t, opts.applyGenerationDefaults())
	})
}

func TestOptionsDependencyDirs(T *testing.T) {
	T.Parallel()

	T.Run("returns what it was given when no prefixes are named", func(t *testing.T) {
		t.Parallel()

		opts := Options{DependencyDirs: map[string]string{"example.com/dep": "/tmp/dep"}}

		dirs, err := opts.dependencyDirs(t.Context())

		must.NoError(t, err)
		test.Eq(t, map[string]string{"example.com/dep": "/tmp/dep"}, dirs)
	})

	T.Run("finds a vendored module's source in the vendor directory", func(t *testing.T) {
		t.Parallel()

		dir := writeVendorTree(t, []string{"example.com/dep", "example.com/other"}, []string{"example.com/dep", "example.com/other"})

		opts := Options{Dir: dir, Dependencies: []string{"example.com/dep"}}

		dirs, err := opts.dependencyDirs(t.Context())

		must.NoError(t, err)
		test.Eq(t, map[string]string{"example.com/dep": filepath.Join(dir, "vendor", "example.com", "dep")}, dirs)
	})

	T.Run("reports a prefix that matches nothing, rather than generating a short file", func(t *testing.T) {
		t.Parallel()

		dir := writeVendorTree(t, []string{"example.com/other"}, []string{"example.com/other"})

		opts := Options{Dir: dir, Dependencies: []string{"example.com/dep"}}

		_, err := opts.dependencyDirs(t.Context())

		test.Error(t, err)
	})

	T.Run("reports a module listed but not vendored", func(t *testing.T) {
		t.Parallel()

		dir := writeVendorTree(t, []string{"example.com/dep"}, nil)

		opts := Options{Dir: dir, Dependencies: []string{"example.com/dep"}}

		_, err := opts.dependencyDirs(t.Context())

		test.Error(t, err)
	})

	T.Run("prefers an explicitly given directory to a discovered one", func(t *testing.T) {
		t.Parallel()

		dir := writeVendorTree(t, []string{"example.com/dep"}, []string{"example.com/dep"})

		opts := Options{
			Dir:            dir,
			Dependencies:   []string{"example.com/dep"},
			DependencyDirs: map[string]string{"example.com/dep": "/elsewhere"},
		}

		dirs, err := opts.dependencyDirs(t.Context())

		must.NoError(t, err)
		test.EqOp(t, "/elsewhere", dirs["example.com/dep"])
	})

	T.Run("falls back to the module graph when there is no vendor directory", func(t *testing.T) {
		t.Parallel()

		dir := writeMinimalModule(t)

		opts := Options{Dir: dir, Dependencies: []string{"example.com/minimal"}}

		dirs, err := opts.dependencyDirs(t.Context())

		must.NoError(t, err)
		test.MapContainsKey(t, dirs, "example.com/minimal")
	})
}
