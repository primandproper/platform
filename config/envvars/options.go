package envvars

import (
	"bufio"
	"context"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/primandproper/platform-go/v10/errors"
)

// Options configures Collect and Generate.
//
// Exactly one of UnionKey and Roots names the configuration structs to walk.
// Both are written as "<package>.<TypeName>", where <package> is the
// module-relative directory the type is declared in ("internal/config") for a
// type in Dir's own module, and the full import path
// ("github.com/example/dep/config") for one in a dependency.
type Options struct {
	// DependencyDirs maps a module's import path to the directory holding its
	// source. It is for callers that already know where a module's source is —
	// a vendored tree, a checkout under test — and is honored whether or not
	// the module's path matches a Dependencies prefix.
	DependencyDirs map[string]string

	// Dir is the root of the module whose configuration structs are read: the
	// directory holding its go.mod. It defaults to the working directory.
	Dir string

	// Prefix is prepended to every derived variable name, and is the same
	// prefix the loader is given via config.WithPrefix. It does not appear in
	// the generated constant's identifier, only in its value.
	Prefix string

	// UnionKey names the generic type constraint whose members are the loadable
	// configuration structs, e.g. "internal/config.configurations". Prefer it to
	// Roots: it is what makes the output complete by construction.
	UnionKey string

	// OutputPath is the file Generate writes, resolved against Dir unless it is
	// absolute. Collect ignores it.
	OutputPath string

	// Package is the package clause of the generated file. It defaults to the
	// name of the directory OutputPath sits in. Collect ignores it.
	Package string

	// Roots names the configuration structs to walk directly. Give this only
	// when there is no constraint to derive them from; unlike UnionKey it goes
	// stale silently, which is the failure this package exists to remove.
	Roots []string

	// Dependencies bounds which dependency modules are parsed alongside Dir's
	// own source, as module path prefixes ("github.com/primandproper/platform-go").
	// Most of a service's configuration is usually assembled out of structs
	// declared by a library, and a field whose type this package cannot find is
	// a field whose variables go missing — so a prefix that matches no module in
	// the graph is an error rather than an empty result.
	//
	// Modules are located from Dir's vendor directory when it has one, and from
	// `go list -m all` otherwise.
	Dependencies []string
}

// applyDefaults fills in what Collect can decide for itself, and reports what
// it cannot proceed without.
func (o *Options) applyDefaults() error {
	if o.Dir == "" {
		o.Dir = "."
	}

	switch {
	case o.UnionKey == "" && len(o.Roots) == 0:
		return errors.New("one of UnionKey or Roots is required to know which configuration structs to walk")
	case o.UnionKey != "" && len(o.Roots) > 0:
		return errors.New("only one of UnionKey or Roots may be given")
	}

	return nil
}

// applyGenerationDefaults fills in the defaults Generate needs on top of the
// ones Collect needs.
func (o *Options) applyGenerationDefaults() error {
	if err := o.applyDefaults(); err != nil {
		return err
	}

	if o.OutputPath == "" {
		return errors.New("OutputPath is required")
	}

	if o.Package == "" {
		o.Package = filepath.Base(filepath.Dir(o.outputPath()))
	}

	if !token.IsIdentifier(o.Package) || token.Lookup(o.Package).IsKeyword() {
		return errors.Newf("package name %q derived for the generated file is not a valid Go identifier", o.Package)
	}

	return nil
}

// outputPath resolves OutputPath against Dir.
func (o *Options) outputPath() string {
	if filepath.IsAbs(o.OutputPath) {
		return o.OutputPath
	}

	return filepath.Join(o.Dir, o.OutputPath)
}

// dependencyDirs returns the source directory of every dependency module to
// parse, keyed by import path. DependencyDirs wins over anything discovered,
// so a caller can point this package at a checkout it would not otherwise find.
func (o *Options) dependencyDirs(ctx context.Context) (map[string]string, error) {
	dirs := make(map[string]string, len(o.DependencyDirs))
	maps.Copy(dirs, o.DependencyDirs)

	if len(o.Dependencies) == 0 {
		return dirs, nil
	}

	discovered, err := o.discoverModules(ctx)
	if err != nil {
		return nil, err
	}

	for importPath, dir := range discovered {
		if !matchesAny(importPath, o.Dependencies) {
			continue
		}

		if _, given := dirs[importPath]; !given {
			dirs[importPath] = dir
		}
	}

	for _, prefix := range o.Dependencies {
		if !anyMatches(dirs, prefix) {
			return nil, errors.Newf("no module whose path begins with %q was found for %q; it is required by Dependencies", prefix, o.Dir)
		}
	}

	return dirs, nil
}

// discoverModules locates the source of every module Dir depends on. A vendored
// module has its source in the vendor directory and is not in the module cache,
// and `go list -m all` refuses to answer at all while a vendor directory is
// present, so the vendor directory is read directly when there is one.
func (o *Options) discoverModules(ctx context.Context) (map[string]string, error) {
	vendorDir := filepath.Join(o.Dir, "vendor")
	if _, err := os.Stat(filepath.Join(vendorDir, "modules.txt")); err == nil {
		return vendoredModules(vendorDir)
	}

	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-f", "{{.Path}}\t{{.Dir}}", "all")
	cmd.Dir = o.Dir

	out, err := cmd.Output()
	if err != nil {
		return nil, errors.Wrapf(err, "listing the modules %q depends on", o.Dir)
	}

	dirs := map[string]string{}

	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		importPath, dir, found := strings.Cut(line, "\t")
		if !found || dir == "" {
			// A module in the graph whose source has not been downloaded has no
			// directory to parse. Whether that matters is decided by
			// dependencyDirs, which reports a prefix that ends up matching
			// nothing.
			continue
		}

		dirs[importPath] = dir
	}

	return dirs, nil
}

// vendoredModules reads the module list out of a vendor directory's
// modules.txt, mapping each module's path to the directory its packages were
// vendored into.
func vendoredModules(vendorDir string) (map[string]string, error) {
	f, err := os.Open(filepath.Join(vendorDir, "modules.txt"))
	if err != nil {
		return nil, errors.Wrap(err, "opening vendor/modules.txt")
	}

	defer func() {
		_ = f.Close() //nolint:errcheck // read-only file; close error is not actionable here
	}()

	dirs := map[string]string{}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// Module lines read "# <path> <version>", optionally with a "=> ..."
		// replacement following. Package lines carry no marker, and "## " lines
		// are annotations such as "## explicit".
		line := scanner.Text()
		if !strings.HasPrefix(line, "# ") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		dir := filepath.Join(vendorDir, filepath.FromSlash(fields[1]))
		//nolint:gosec // G703: the path is built from this module's own vendor/modules.txt, and is only stat'ed
		if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
			continue
		}

		dirs[fields[1]] = dir
	}

	if err = scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "scanning vendor/modules.txt")
	}

	return dirs, nil
}

func matchesAny(importPath string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(importPath, prefix) {
			return true
		}
	}

	return false
}

func anyMatches(dirs map[string]string, prefix string) bool {
	for importPath := range dirs {
		if strings.HasPrefix(importPath, prefix) {
			return true
		}
	}

	return false
}
