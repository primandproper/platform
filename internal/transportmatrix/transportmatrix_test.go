package transportmatrix_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// transportDirs are the directory names that make a package a transport. A
// package is one because it ships one of these, not because it says so
// anywhere, which is what lets this walk catch the package that grew handlers
// without telling the README.
var transportDirs = []string{"http", "grpc"}

// kinds are the closed set a row's second column may name. It is closed on
// purpose: a new kind is a new category of thing this module ships across the
// boundary, which is a decision to write down in the section's prose rather
// than a word to invent in a cell. Adding one here is the cheap half of making
// that decision.
var kinds = []string{"server", "mapping", "wire conversion", "middleware", "binding", "resource surface"}

// sectionHeading is the README section this package is about. The table read is
// the first one beneath it.
const sectionHeading = "## Stores and Transports"

// transportRow matches one row of the table: a backticked package path, the
// kind, and the prose. The prose column is matched but unread — it is there for
// the reader, and a check on it would be a check on wording.
var transportRow = regexp.MustCompile("^\\|\\s*`([a-z0-9/]+)`\\s*\\|([^|]*)\\|([^|]*)\\|\\s*$")

// backticked matches every backticked span in the section, which is how the
// store-only claim is read: the section names those packages in prose rather
// than in a row, because "ships nothing" has no columns to fill.
var backticked = regexp.MustCompile("`([^`]+)`")

// TestEveryTransportHasARow is the entry this package exists to make impossible
// to forget. A package that ships an http or grpc subpackage and appears in no
// row has moved the boundary without the README learning about it, which is the
// state the section was written to end.
func TestEveryTransportHasARow(T *testing.T) {
	T.Parallel()

	rows := table(T)

	for _, pkg := range shippedTransports(T) {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			_, ok := rows[pkg]
			must.True(t, ok, must.Sprintf(
				"%s ships a transport and has no row in the README's Stores and Transports table", pkg))
		})
	}
}

// TestNoRowOutlivesItsTransport is the other direction, and the one a rename or
// a deletion breaks. A row naming a transport that is no longer there reads
// exactly like a live one, and tells a consumer not to write a handler this
// module has stopped shipping.
func TestNoRowOutlivesItsTransport(T *testing.T) {
	T.Parallel()

	shipped := shippedTransports(T)

	for pkg := range table(T) {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			test.True(t, slices.Contains(shipped, pkg), test.Sprintf(
				"%s has a row in the Stores and Transports table and ships no http or grpc subpackage", pkg))
		})
	}
}

// TestKindsComeFromTheClosedSet keeps the second column from becoming free
// text. The column is what makes the table readable at a glance — a consumer
// scanning for "is any of this a resource surface I would have written myself?"
// reads it and nothing else — and one row calling itself an "adapter" is a row
// that answers a different question than the rest.
func TestKindsComeFromTheClosedSet(T *testing.T) {
	T.Parallel()

	for pkg, kind := range table(T) {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			test.True(t, slices.Contains(kinds, kind), test.Sprintf(
				"%s is a %q, which is not one of %v", pkg, kind, kinds))
		})
	}
}

// TestStoreOnlyPackagesShipNoTransport checks the claim the section is actually
// for. The transports are listed and therefore self-correcting; the packages
// named as shipping a store and no handlers are the ones a consumer plans a
// port around, and a single new subdirectory reverses that claim without
// touching a row.
//
// The set is read out of the section's prose rather than listed here, so a
// package added to the sentence is checked by the same test that checks the
// ten already in it.
func TestStoreOnlyPackagesShipNoTransport(T *testing.T) {
	T.Parallel()

	claimed := storeOnly(T)

	must.SliceNotEmpty(T, claimed, must.Sprint(
		"no store-only packages read out of the Stores and Transports section"))

	shipped := shippedTransports(T)

	for _, pkg := range claimed {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			for _, dir := range transportDirs {
				must.DirNotExists(t, filepath.Join(moduleRootPath(), filepath.FromSlash(pkg), dir), must.Sprintf(
					"the README says %s ships a store and no handlers, and it ships a %s subpackage", pkg, dir))
			}

			// The walk is consulted as well as the filesystem, because a
			// transport nested deeper than one level — a settings/admin/http,
			// say — is still a handler this module ships and is still a
			// sentence in the README that has stopped being true.
			for _, transport := range shipped {
				test.False(t, strings.HasPrefix(transport, pkg+"/"), test.Sprintf(
					"the README says %s ships a store and no handlers, and %s exists", pkg, transport))
			}
		})
	}
}

// TestSectionIsReadable guards the parse itself. Every test above reads an empty
// table as agreement, so a section reformatted past the row pattern would turn
// all of them green at once.
func TestSectionIsReadable(T *testing.T) {
	T.Parallel()

	rows := table(T)

	test.MapNotEmpty(T, rows, test.Sprint("no rows parsed from the README's Stores and Transports table"))

	// The two servers are the rows most certain to be there: this module cannot
	// stop shipping the thing that binds the port. A parse that finds a table
	// without them found something else.
	for _, pkg := range []string{"server/http", "server/grpc"} {
		_, ok := rows[pkg]
		test.True(T, ok, test.Sprintf("%s has no row, which means the table parsed is not the one intended", pkg))
	}
}

// TestModuleRootIsThisModule keeps every walk above honest. A test binary run
// from anywhere but this package's directory would walk a tree with no go.mod
// at its root, find no transports, and report a table that matches nothing as a
// table that matches everything.
func TestModuleRootIsThisModule(T *testing.T) {
	T.Parallel()

	must.FileExists(T, filepath.Join(moduleRootPath(), "go.mod"))
	must.FileExists(T, filepath.Join(moduleRootPath(), "README.md"))
}

// parsedSection is the section read apart: the rows of its table, and the
// packages its prose names as shipping a store and no handlers.
type parsedSection struct {
	rows      map[string]string
	storeOnly []string
}

// section is the README's Stores and Transports section, read once: five tests
// read it, and the answer is a property of the file rather than of whichever
// test asked first.
var section = sync.OnceValues(func() (parsedSection, error) {
	body, err := os.ReadFile(filepath.Join(moduleRootPath(), "README.md"))
	if err != nil {
		return parsedSection{}, err
	}

	return readSection(string(body)), nil
})

func table(t *testing.T) map[string]string {
	t.Helper()

	parsed, err := section()
	must.NoError(t, err)

	return parsed.rows
}

func storeOnly(t *testing.T) []string {
	t.Helper()

	parsed, err := section()
	must.NoError(t, err)

	return parsed.storeOnly
}

// readSection reads the section into its rows and the packages its prose names
// as store-only.
//
// The prose side takes every backticked span that names a directory at the
// module root and has no row, which is deliberately narrower than "every
// backticked span". A path with a slash is skipped because the section's prose
// mentions webhooks/inbound, the one transport that is not an http subpackage
// and therefore not a row; a span naming no directory is skipped because the
// prose also backticks type names and header names.
func readSection(body string) parsedSection {
	rows := map[string]string{}
	named := map[string]struct{}{}

	var inSection bool

	for line := range strings.Lines(body) {
		line = strings.TrimRight(line, "\n")

		if strings.HasPrefix(line, "#") {
			if inSection && line != sectionHeading {
				break
			}

			inSection = line == sectionHeading

			continue
		}

		if !inSection {
			continue
		}

		if match := transportRow.FindStringSubmatch(line); match != nil {
			rows[match[1]] = strings.TrimSpace(match[2])

			continue
		}

		for _, match := range backticked.FindAllStringSubmatch(line, -1) {
			named[match[1]] = struct{}{}
		}
	}

	prose := make([]string, 0, len(named))

	for name := range named {
		if _, isRow := rows[name]; isRow || strings.Contains(name, "/") {
			continue
		}

		if info, err := os.Stat(filepath.Join(moduleRootPath(), name)); err == nil && info.IsDir() {
			prose = append(prose, name)
		}
	}

	slices.Sort(prose)

	return parsedSection{rows: rows, storeOnly: prose}
}

// shippedTransports is every package in the module with an http or grpc
// subpackage, as the slash-separated paths the README's rows are written in.
var walked = sync.OnceValues(func() ([]string, error) {
	var found []string

	err := walkModule(func(path string, d fs.DirEntry) error {
		if !d.IsDir() || !slices.Contains(transportDirs, d.Name()) {
			return nil
		}

		pkg, err := packagePath(path)
		if err != nil {
			return err
		}

		found = append(found, pkg)

		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(found)

	return found, nil
})

func shippedTransports(t *testing.T) []string {
	t.Helper()

	found, err := walked()
	must.NoError(t, err)

	return found
}

// walkModule visits every entry in the module, skipping the directories that
// hold no packages of its own.
func walkModule(visit func(path string, d fs.DirEntry) error) error {
	root := moduleRootPath()

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Dot directories hold no packages of this module's, and one of them
		// holds other checkouts of it: an agent worktree under .claude would
		// otherwise report every transport in the module twice. testdata holds
		// fixture trees that belong to no package.
		if d.IsDir() {
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "artifacts" || name == "testdata") {
				return filepath.SkipDir
			}
		}

		return visit(path, d)
	})
}

// packagePath is path relative to the module root, in the slash-separated form
// the README's rows are written in.
func packagePath(path string) (string, error) {
	rel, err := filepath.Rel(moduleRootPath(), path)
	if err != nil {
		return "", err
	}

	return filepath.ToSlash(rel), nil
}

// moduleRootPath is two directories up, which is where this package sits and
// where go.mod has to be for the answer to be this module rather than whatever
// tree a test binary was copied into.
var moduleRootPath = sync.OnceValue(func() string {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		panic(err)
	}

	return root
})
