package tiermatrix_test

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

// tiers are the closed set a row's first column may name. It is closed because
// the sort has two answers by construction: a package is what every service is
// built from, or it is what one product has. A third value in that column is a
// third tier, which is a decision to make in the section's prose rather than a
// word to invent in a cell.
var tiers = []string{"primitives-go", "platform-go"}

// skipped are the root directories the sort does not cover. internal is this
// module's own workings rather than anything a consumer imports; artifacts is
// build output; a dot directory holds no packages of this module's, and one of
// them holds other checkouts of it.
var skipped = []string{"internal", "artifacts", "testdata"}

// sectionHeading is the README section this package is about. The table read is
// the first one beneath it, and the parse stops at the next heading — which is
// the Transports subsection, whose own table belongs to internal/transportmatrix.
const sectionHeading = "## Primitives and Domains"

// tierRow matches one row of the table: the backticked tier, the prose naming
// what the row is, and the packages. The middle column is matched but unread —
// it is there for the reader, and a check on it would be a check on wording.
var tierRow = regexp.MustCompile("^\\|\\s*`([a-z-]+)`\\s*\\|([^|]*)\\|(.*)\\|\\s*$")

// backticked matches every backticked span in a row's package column, which is
// the only place package names are read from. The section's prose backticks
// package names too — the rule quotes several as examples — and reading those
// would make an example an entry.
var backticked = regexp.MustCompile("`([^`]+)`")

// TestEveryPackageHasATier is the entry this package exists to make impossible
// to forget. A top-level package in no tier is a package the rule was never
// applied to, which is the state the section was written to end: the next
// package proposed has something to be measured against only if the last one
// was.
func TestEveryPackageHasATier(T *testing.T) {
	T.Parallel()

	tiered := entries(T)

	for _, pkg := range topLevelPackages(T) {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			_, ok := tiered[pkg]
			must.True(t, ok, must.Sprintf(
				"%s is a top-level package and has no tier in the README's Primitives and Domains table", pkg))
		})
	}
}

// TestNoEntryOutlivesItsPackage is the other direction, and the one a rename or
// a deletion breaks. An entry naming a package that is no longer there reads
// exactly like a live one, and answers "where would this go?" with a tier for
// something that went nowhere.
func TestNoEntryOutlivesItsPackage(T *testing.T) {
	T.Parallel()

	for pkg := range entries(T) {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			test.DirExists(t, filepath.Join(moduleRootPath(), filepath.FromSlash(pkg)), test.Sprintf(
				"%s has a tier in the Primitives and Domains table and is not a directory in this module", pkg))
		})
	}
}

// TestNothingInThePrimitivesTierOwnsATable checks the rule's one hard claim.
// Everything else the rule says is a judgement — whether a package is a
// provider, whether an application with no users would still need it — but
// "Nothing in it owns a table" is settled by a directory listing, and a
// primitive that has grown one is a package that changed tiers without anybody
// deciding to move it.
//
// Ownership is read by longest prefix, because that is what nesting means here:
// authentication is a primitive, authentication/passwordreset owns a table, and
// the migrations under the second do not make a store of the first.
func TestNothingInThePrimitivesTierOwnsATable(T *testing.T) {
	T.Parallel()

	tiered := entries(T)

	for _, pkg := range schemaOwners(T) {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			owner, ok := longestPrefix(tiered, pkg)
			must.True(t, ok, must.Sprintf(
				"%s ships DDL and neither it nor any package above it has a tier", pkg))

			test.EqOp(t, "platform-go", tiered[owner], test.Sprintf(
				"%s ships DDL and the nearest tier above it is %s, which the README puts in %s",
				pkg, owner, tiered[owner]))
		})
	}
}

// TestTiersComeFromTheClosedSet keeps the first column from becoming free text.
// The column is the whole answer a reader came for, and one row saying "core" or
// "shared" is a row proposing a tier the rule does not have.
func TestTiersComeFromTheClosedSet(T *testing.T) {
	T.Parallel()

	for pkg, tier := range entries(T) {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			test.True(t, slices.Contains(tiers, tier), test.Sprintf(
				"%s is in %q, which is not one of %v", pkg, tier, tiers))
		})
	}
}

// TestNoPackageIsInTwoTiers is the failure a table this long invites: a package
// listed under one kind and then, months later, under another, with both rows
// reading true on their own. The sort is a function or it is not a sort.
func TestNoPackageIsInTwoTiers(T *testing.T) {
	T.Parallel()

	parsed, err := section()
	must.NoError(T, err)

	test.SliceEmpty(T, parsed.duplicates, test.Sprintf(
		"%v appear more than once in the Primitives and Domains table", parsed.duplicates))
}

// TestSectionIsReadable guards the parse itself. Every test above reads an empty
// table as agreement, so a section reformatted past the row pattern would turn
// all of them green at once.
func TestSectionIsReadable(T *testing.T) {
	T.Parallel()

	tiered := entries(T)

	test.MapNotEmpty(T, tiered, test.Sprint("no entries parsed from the README's Primitives and Domains table"))

	// One anchor per tier, each the package its side of the rule names in the
	// rule's own words: the schema tooling stores are built with, and the noun
	// with a users table. A parse that finds a table without them found
	// something else.
	test.EqOp(T, "primitives-go", tiered["database"], test.Sprint("database is not where the rule says it is"))
	test.EqOp(T, "platform-go", tiered["identity"], test.Sprint("identity is not where the rule says it is"))
}

// TestModuleRootIsThisModule keeps every walk above honest. A test binary run
// from anywhere but this package's directory would walk a tree with no go.mod
// at its root, find no packages and no DDL, and report a table that matches
// nothing as a table that matches everything.
func TestModuleRootIsThisModule(T *testing.T) {
	T.Parallel()

	must.FileExists(T, filepath.Join(moduleRootPath(), "go.mod"))
	must.FileExists(T, filepath.Join(moduleRootPath(), "README.md"))
}

// parsedSection is the section's table read apart: each package's tier, and the
// packages that turned up in more than one row.
type parsedSection struct {
	entries    map[string]string
	duplicates []string
}

// section is the README's Primitives and Domains section, read once: six tests
// read it, and the answer is a property of the file rather than of whichever
// test asked first.
var section = sync.OnceValues(func() (parsedSection, error) {
	body, err := os.ReadFile(filepath.Join(moduleRootPath(), "README.md"))
	if err != nil {
		return parsedSection{}, err
	}

	return readSection(string(body)), nil
})

func entries(t *testing.T) map[string]string {
	t.Helper()

	parsed, err := section()
	must.NoError(t, err)

	return parsed.entries
}

// readSection reads the section's table into one tier per package.
func readSection(body string) parsedSection {
	parsed := parsedSection{entries: map[string]string{}}

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

		match := tierRow.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		for _, pkg := range backticked.FindAllStringSubmatch(match[3], -1) {
			if _, seen := parsed.entries[pkg[1]]; seen {
				parsed.duplicates = append(parsed.duplicates, pkg[1])

				continue
			}

			parsed.entries[pkg[1]] = match[1]
		}
	}

	slices.Sort(parsed.duplicates)

	return parsed
}

// longestPrefix is the entry that owns pkg: pkg itself, or the longest entry it
// sits beneath.
func longestPrefix(tiered map[string]string, pkg string) (string, bool) {
	var (
		owner string
		found bool
	)

	for entry := range tiered {
		if entry != pkg && !strings.HasPrefix(pkg, entry+"/") {
			continue
		}

		if len(entry) > len(owner) {
			owner, found = entry, true
		}
	}

	return owner, found
}

// topLevelPackages is every directory at the module root the sort covers, which
// is every directory that is not one of the few holding no package a consumer
// could import.
var rootDirs = sync.OnceValues(func() ([]string, error) {
	read, err := os.ReadDir(moduleRootPath())
	if err != nil {
		return nil, err
	}

	var found []string

	for _, entry := range read {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || slices.Contains(skipped, entry.Name()) {
			continue
		}

		found = append(found, entry.Name())
	}

	slices.Sort(found)

	return found, nil
})

func topLevelPackages(t *testing.T) []string {
	t.Helper()

	found, err := rootDirs()
	must.NoError(t, err)

	must.SliceNotEmpty(t, found, must.Sprint("no top-level packages found in this module"))

	return found
}

// schemaOwners is every package in the module shipping a migrations directory,
// as the slash-separated paths the table's entries are written in. It is the
// same ground truth internal/dialectmatrix reads, and for the same reason: a
// package owns a table because it ships the DDL for one, not because it says so
// anywhere.
var walked = sync.OnceValues(func() ([]string, error) {
	var found []string

	err := walkModule(func(path string, d fs.DirEntry) error {
		if !d.IsDir() || d.Name() != "migrations" {
			return nil
		}

		pkg, err := packagePath(filepath.Dir(path))
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

func schemaOwners(t *testing.T) []string {
	t.Helper()

	found, err := walked()
	must.NoError(t, err)

	must.SliceNotEmpty(t, found, must.Sprint("no packages shipping DDL found in this module"))

	return found
}

// walkModule visits every entry in the module, skipping the directories that
// hold no packages of its own. testdata is skipped because the migration
// tooling's fixtures are migrations directories belonging to no package.
func walkModule(visit func(path string, d fs.DirEntry) error) error {
	root := moduleRootPath()

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || slices.Contains(skipped, name)) {
				return filepath.SkipDir
			}
		}

		return visit(path, d)
	})
}

// packagePath is path relative to the module root, in the slash-separated form
// the table's entries are written in.
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
