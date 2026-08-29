package dialectmatrix_test

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

// dialects are the three the module speaks, in the order the README's columns
// carry them. The order is load-bearing: a row is read positionally, so a
// column inserted or reordered in the table without changing this list is a
// failing test rather than a matrix that has silently started reporting MySQL
// support as SQLite's.
var dialects = []string{"postgres", "mysql", "sqlite"}

// supported and unsupported are the two marks a cell may hold. Anything else in
// a cell is rejected rather than read as one of them, so a row half-filled with
// a "planned" or a "partial" fails here instead of quietly counting as a no.
const (
	supported   = "✓"
	unsupported = "—"
)

// matrixHeading is the README section this package is about. The table read is
// the first one beneath it.
const matrixHeading = "## SQL Dialect Support"

// matrixRow matches one row of the table: a backticked package path followed by
// one cell per dialect.
var matrixRow = regexp.MustCompile("^\\|\\s*`([a-z0-9/]+)`\\s*\\|([^|]*)\\|([^|]*)\\|([^|]*)\\|\\s*$")

// generatedQueries matches the per-dialect file names sqlc-gen-unison emits.
// postgresql is sqlc's spelling of the engine; this module's dialect is called
// postgres, and the two are reconciled here rather than in the README, which
// names the dialect a consumer configures.
var generatedQueries = regexp.MustCompile(`^queries_(postgresql|mysql|sqlite)_generated\.go$`)

// TestEveryDDLShippingPackageHasARow is the entry this package exists to make
// impossible to forget. A package that ships table DDL and appears in no row is
// a dialect roster a consumer can only discover at wiring time, which is the
// state the matrix was written to end.
func TestEveryDDLShippingPackageHasARow(T *testing.T) {
	T.Parallel()

	rows := matrix(T)

	for pkg, shipped := range shippedDDL(T) {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			_, ok := rows[pkg]
			must.True(t, ok, must.Sprintf(
				"%s ships DDL for %s and has no row in the README's SQL Dialect Support matrix",
				pkg, strings.Join(shipped, ", ")))
		})
	}
}

// TestNoRowOutlivesItsPackage is the other direction, and the one a rename or a
// deletion breaks. A row naming a package that ships no DDL reads exactly like
// a live entry, and answers "can I run this on MySQL?" about something that is
// no longer there.
func TestNoRowOutlivesItsPackage(T *testing.T) {
	T.Parallel()

	shipped := shippedDDL(T)

	for pkg := range matrix(T) {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			_, ok := shipped[pkg]
			test.True(t, ok, test.Sprintf(
				"%s has a row in the SQL Dialect Support matrix and ships no DDL in this module", pkg))
		})
	}
}

// TestRowsMatchTheShippedDDL is the check the table is for. A package that gains
// or loses a dialect changes the files in its migrations directory, and the row
// that still claims the old roster fails here rather than being believed.
func TestRowsMatchTheShippedDDL(T *testing.T) {
	T.Parallel()

	shipped := shippedDDL(T)

	for pkg, claimed := range matrix(T) {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			have, ok := shipped[pkg]
			must.True(t, ok, must.Sprintf("%s ships no DDL", pkg))
			test.Eq(t, have, claimed, test.Sprintf(
				"%s: the matrix claims %v and its migrations ship %v", pkg, claimed, have))
		})
	}
}

// TestRowsMatchTheGeneratedQueriers is the second ground truth, and it is not
// the first one restated. DDL and queries are generated from separate inputs, so
// a package can ship a schema for a dialect it executes nothing against — a
// migration that creates tables no querier will ever read. A row that matches
// both is a roster a consumer can act on.
//
// A package with no generated querier is passed over rather than failed: the
// tier is something packages are ported onto, and one still composing its SQL in
// Go has a roster its DDL is the only record of. internal/sqltier is where that
// state is enumerated and where a port is tracked.
func TestRowsMatchTheGeneratedQueriers(T *testing.T) {
	T.Parallel()

	queriers := generatedQueriers(T)

	for pkg, claimed := range matrix(T) {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			have, ok := queriers[pkg]
			if !ok {
				t.Skip("composes its SQL in Go; see internal/sqltier")
			}

			test.Eq(t, have, claimed, test.Sprintf(
				"%s: the matrix claims %v and its generated querier covers %v", pkg, claimed, have))
		})
	}
}

// TestNarrowedPackagesPointAtTheMatrix is the half of this that is about the
// documentation rather than the code. A package that narrows the three-dialect
// promise used to be the only home of its own narrowing: the reasoning was
// sound and sat in a doc.go nobody reads until they have already chosen the
// package. The matrix is the module-level answer, and a narrowed package's doc
// has to hand the reader to it.
//
// It is derived from the table rather than listed here, so a fourth package
// that narrows is caught by the same test that catches the three.
func TestNarrowedPackagesPointAtTheMatrix(T *testing.T) {
	T.Parallel()

	for pkg, claimed := range matrix(T) {
		if len(claimed) == len(dialects) {
			continue
		}

		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			doc, err := os.ReadFile(filepath.Join(moduleRootPath(), filepath.FromSlash(pkg), "doc.go"))
			must.NoError(t, err, must.Sprintf(
				"%s supports only %v and has no doc.go to point at the matrix from", pkg, claimed))

			// Whitespace is collapsed before the search because a doc comment
			// is wrapped to eighty columns and the phrase is three words long:
			// the next author to reflow the paragraph would otherwise break
			// this test by moving a line ending, which says nothing about
			// whether the pointer is still there.
			test.StrContains(t, collapse(string(doc)), strings.TrimPrefix(matrixHeading, "## "), test.Sprintf(
				"%s supports only %v and its doc does not name the README's matrix, leaving itself the only home of its own narrowing",
				pkg, claimed))
		})
	}
}

// TestMatrixIsReadable guards the parse itself. Every test above reads an empty
// matrix as agreement, so a table that has been reformatted past the row pattern
// would turn all of them green at once.
func TestMatrixIsReadable(T *testing.T) {
	T.Parallel()

	rows := matrix(T)

	test.MapNotEmpty(T, rows, test.Sprint("no rows parsed from the README's SQL Dialect Support matrix"))

	for pkg, claimed := range rows {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			test.SliceContains(t, claimed, "postgres", test.Sprintf(
				"%s supports no Postgres; every SQL package in this module does", pkg))
		})
	}
}

// matrix is the README's table, read as package path to the dialects its row
// ticks. Parsed once: five tests read it, and the answer is a property of the
// file rather than of whichever test asked first.
var parsed = sync.OnceValues(func() (map[string][]string, error) {
	body, err := os.ReadFile(filepath.Join(moduleRootPath(), "README.md"))
	if err != nil {
		return nil, err
	}

	return readMatrix(string(body)), nil
})

func matrix(t *testing.T) map[string][]string {
	t.Helper()

	rows, err := parsed()
	must.NoError(t, err)

	return rows
}

// readMatrix reads the rows of the first table beneath the matrix heading.
//
// It stops at the next heading of any level rather than at the end of the table,
// so a second table added to the section is read too — a matrix split in two is
// still one matrix, and half of it silently going unchecked is the failure this
// package is here to prevent.
func readMatrix(body string) map[string][]string {
	rows := map[string][]string{}

	var inSection bool

	for line := range strings.Lines(body) {
		line = strings.TrimRight(line, "\n")

		if strings.HasPrefix(line, "#") {
			if inSection && line != matrixHeading {
				break
			}

			inSection = line == matrixHeading

			continue
		}

		if !inSection {
			continue
		}

		match := matrixRow.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		var claimed []string

		for i, d := range dialects {
			if strings.TrimSpace(match[i+2]) == supported {
				claimed = append(claimed, d)
			}
		}

		rows[match[1]] = claimed
	}

	return rows
}

// shippedDDL is every package in the module whose migrations subpackage embeds a
// per-dialect schema, mapped to the dialects it embeds.
//
// Names outside the three are ignored rather than reported: webhooks ships
// upgrade_<dialect>.sql beside its schema, which is a migration for an existing
// table rather than a dialect the package supports.
var walkedDDL = sync.OnceValues(func() (map[string][]string, error) {
	found := map[string][]string{}

	err := walkModule(func(path string, d fs.DirEntry) error {
		if !d.IsDir() || d.Name() != "migrations" {
			return nil
		}

		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}

		var shipped []string

		for _, entry := range entries {
			if name, ok := strings.CutSuffix(entry.Name(), ".sql"); ok && slices.Contains(dialects, name) {
				shipped = append(shipped, name)
			}
		}

		if len(shipped) == 0 {
			return nil
		}

		pkg, err := packagePath(filepath.Dir(path))
		if err != nil {
			return err
		}

		found[pkg] = order(shipped)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return found, nil
})

func shippedDDL(t *testing.T) map[string][]string {
	t.Helper()

	found, err := walkedDDL()
	must.NoError(t, err)

	return found
}

// walkedQueriers is every generated querier in the module, keyed by the package
// that owns the migrations directory above it, mapped to the dialects it was
// emitted for.
//
// Attribution is by longest matching DDL-shipping prefix rather than by the
// generated package's own directory, since the querier lives in an internal
// subpackage several levels beneath the store — identity/internal/identitydb
// belongs to identity, and authentication/webauthn/database/internal/webauthndb
// to authentication/webauthn/database rather than to authentication.
var walkedQueriers = sync.OnceValues(func() (map[string][]string, error) {
	owners, err := walkedDDL()
	if err != nil {
		return nil, err
	}

	found := map[string][]string{}

	err = walkModule(func(path string, d fs.DirEntry) error {
		if d.IsDir() {
			return nil
		}

		match := generatedQueries.FindStringSubmatch(d.Name())
		if match == nil {
			return nil
		}

		emitted := match[1]
		if emitted == "postgresql" {
			emitted = "postgres"
		}

		dir, pathErr := packagePath(filepath.Dir(path))
		if pathErr != nil {
			return pathErr
		}

		owner, ok := owningPackage(dir, owners)
		if !ok {
			return nil
		}

		if !slices.Contains(found[owner], emitted) {
			found[owner] = order(append(found[owner], emitted))
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return found, nil
})

func generatedQueriers(t *testing.T) map[string][]string {
	t.Helper()

	found, err := walkedQueriers()
	must.NoError(t, err)

	return found
}

// owningPackage is the longest DDL-shipping package that dir sits inside, which
// is the store the querier beneath it belongs to.
func owningPackage(dir string, owners map[string][]string) (string, bool) {
	var owner string

	for candidate := range owners {
		if dir != candidate && !strings.HasPrefix(dir, candidate+"/") {
			continue
		}

		if len(candidate) > len(owner) {
			owner = candidate
		}
	}

	return owner, owner != ""
}

// collapse reduces every run of whitespace to a single space, so a phrase is
// searched for as words rather than as bytes.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// order sorts a roster into the column order, so a comparison against a parsed
// row is about membership rather than about the order a directory was read in.
func order(found []string) []string {
	slices.SortFunc(found, func(a, b string) int {
		return slices.Index(dialects, a) - slices.Index(dialects, b)
	})

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
		// otherwise report every package in the module twice. testdata holds
		// database/migrate's fixture migrations, which belong to no package.
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

// TestModuleRootIsThisModule keeps every walk above honest. A test binary run
// from anywhere but this package's directory would walk a tree with no go.mod
// at its root, find no migrations, and report a matrix that matches nothing as
// a matrix that matches everything.
func TestModuleRootIsThisModule(T *testing.T) {
	T.Parallel()

	must.FileExists(T, filepath.Join(moduleRootPath(), "go.mod"))
	must.FileExists(T, filepath.Join(moduleRootPath(), "README.md"))
}
