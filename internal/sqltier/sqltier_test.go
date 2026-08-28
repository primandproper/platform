package sqltier_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// tier is the answer this file requires of every package that holds SQL. The
// package doc says what each one means; the tests below say what each one costs
// to claim.
type tier int

const (
	unison tier = iota
	porting
	exempt
	none
)

func (t tier) String() string {
	switch t {
	case unison:
		return "unison"
	case porting:
		return "porting"
	case exempt:
		return "exempt"
	case none:
		return "none"
	default:
		return fmt.Sprintf("tier(%d)", int(t))
	}
}

type ruling struct {
	why  string
	tier tier
}

// rulings names every package in this module that holds SQL, and the one that
// was ruled on for holding none. The key is the package's directory, relative
// to the module root.
//
// A reason is required of an exemption and of a package ruled SQL-free, and of
// nothing else: "still composes its SQL in Go, and the port is tracked" is the
// same sentence in every case, and a per-package copy of it would be a place for
// a real reason to hide.
var rulings = map[string]ruling{
	// The tier itself. Each is what sqlc-gen-unison emitted from its package's
	// corpus, which is what every port below is a port onto.
	"identity/internal/identitydb":           {tier: unison},
	"notifications/internal/notificationsdb": {tier: unison},

	// Still composing SQL in Go. Each of these is a tracked port onto the
	// corpus; nothing about the list is a decision, which is why none of them
	// carries a reason.
	"audit":                                {tier: porting},
	"authentication/oauth2server/database": {tier: porting},
	"authentication/webauthn/database":     {tier: porting},
	"authorization/database":               {tier: porting},
	"cryptography/shredding":               {tier: porting},
	"dataprivacy":                          {tier: porting},
	"dataprivacy/auditerasure":             {tier: porting},
	"identity":                             {tier: porting},
	"metering":                             {tier: porting},
	"operations":                           {tier: porting},
	"outbox":                               {tier: porting},
	"retention":                            {tier: porting},
	"saga":                                 {tier: porting},
	"sessions/database":                    {tier: porting},
	"timers":                               {tier: porting},
	"webhooks":                             {tier: porting},
	"workqueue":                            {tier: porting},

	// Not table SQL. The corpus is a set of statements checked against a schema
	// this module ships, and none of these is one.
	"audit/migrations":              {tier: exempt, why: "the DDL a schema ships, including the trigger that makes the log append-only; sqlc reads schemas rather than generating them"},
	"database/dialect":              {tier: exempt, why: "one NOTIFY statement, addressed to a channel rather than to a table"},
	"database/migrate":              {tier: exempt, why: "asks a connection which schema it resolves to; goose owns the bookkeeping table and ships its DDL"},
	"database/mysql/tableaccess":    {tier: exempt, why: "DCL and catalog introspection: sqlc has no spelling for CREATE USER or GRANT, and information_schema is not in any schema this module ships"},
	"database/postgres/tableaccess": {tier: exempt, why: "DCL and catalog introspection: sqlc has no spelling for CREATE USER or GRANT, and pg_roles is not in any schema this module ships"},
	"database/querygen":             {tier: exempt, why: "the generator: its SQL literals are the statements a corpus is rendered from, not statements it executes"},
	"distributedlock/postgres":      {tier: exempt, why: "advisory-lock function calls; the lock is a number the server holds for a session, with no table, schema or projection"},
	"search/vector/pgvector":        {tier: exempt, why: "the index table's name, dimension and metadata column are configuration, so its DDL is issued at run time and nothing committed is left for sqlc to check a statement against"},
	"testutils/containers/pgtest":   {tier: exempt, why: "the schemas and databases a container test isolates itself with, created and dropped by the harness rather than by a store"},

	// Ruled on for holding no SQL. Recorded rather than left absent, so a
	// statement appearing here later is a failing test rather than a silence.
	"filtering": {tier: none, why: "supplies the argument names a rendered statement binds and the conversions that bind them; the keyword a survey counted is a word in a comment"},
}

// TestEverySQLPackageIsClassified is the entry this file exists to make
// impossible to forget. A package that grows a statement lands in no ruling
// until somebody puts it in one, and the decision it needs is which.
func TestEverySQLPackageIsClassified(T *testing.T) {
	T.Parallel()

	for pkg, count := range sqlPackages(T) {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			r, ok := rulings[pkg]
			must.True(t, ok, must.Sprintf("%s holds %d SQL statements and no ruling; it is on the tier, ported onto it, or exempt with a reason", pkg, count))
			test.NotEqOp(t, none, r.tier,
				test.Sprintf("%s is recorded as holding no SQL and holds %d statements", pkg, count))
		})
	}
}

// TestNoRulingOutlivesItsSQL is the other direction, and the one a port breaks.
// A package whose statements have moved into a corpus stops matching here, and
// the entry saying it is still porting has to move with them — an exemption or a
// port that outlives its subject is the same undocumented state read backwards.
func TestNoRulingOutlivesItsSQL(T *testing.T) {
	T.Parallel()

	found := sqlPackages(T)

	for pkg, r := range rulings {
		if r.tier == none {
			continue
		}

		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			_, ok := found[pkg]
			test.True(t, ok, test.Sprintf("%s is recorded as %s and holds no SQL; the ruling outlived its statements", pkg, r.tier))
		})
	}
}

// TestPackagesRuledSQLFreeHoldNoSQL is what makes a ruling of "holds none"
// worth writing down. The alternative is an absence, and an absence goes on
// reading true whatever the package does next.
func TestPackagesRuledSQLFreeHoldNoSQL(T *testing.T) {
	T.Parallel()

	found := sqlPackages(T)

	for pkg, r := range rulings {
		if r.tier != none {
			continue
		}

		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			count, ok := found[pkg]
			test.False(t, ok, test.Sprintf("%s holds %d SQL statements and is recorded as holding none, because %s", pkg, count, r.why))
		})
	}
}

// TestRulingsAreReasonedAndReal keeps an exemption from being where a package
// nobody has thought about goes to be quiet, and keeps a renamed or deleted
// package from leaving a ruling behind that reads as a live one.
func TestRulingsAreReasonedAndReal(T *testing.T) {
	T.Parallel()

	root := moduleRoot(T)

	for pkg, r := range rulings {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(pkg)))
			must.NoError(t, err, must.Sprintf("%s is ruled %s and is not a package in this module", pkg, r.tier))
			test.True(t, info.IsDir(), test.Sprintf("%s is not a directory", pkg))

			if r.tier == exempt || r.tier == none {
				test.NotEqOp(t, "", r.why, test.Sprintf("%s is %s without a reason", pkg, r.tier))
			}
		})
	}
}

// sqlStatementOpening matches a string literal that opens a SQL statement.
//
// Upper case is the discriminator, and it is doing real work: this module writes
// its SQL keywords in caps and its prose in a comment, so a case-insensitive
// match reports "create anthropic provider" as a CREATE and an endpoint named
// "revoke" as a REVOKE. Leading line comments are skipped because a rendered
// corpus opens with sqlc's own -- name: annotation.
var sqlStatementOpening = regexp.MustCompile(`(?s)\A\s*(?:--[^\n]*\n\s*)*(?:SELECT|INSERT\s+INTO|UPDATE|DELETE\s+FROM|WITH|CREATE|DROP|GRANT|REVOKE|TRUNCATE|ALTER)\b`)

// scanned is every package in the module holding at least one such literal,
// mapped to how many it holds. Walked once: four tests read it, and the answer
// is a property of the tree rather than of whichever test asked first.
var scanned = sync.OnceValues(func() (map[string]int, error) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return nil, err
	}

	return scan(root)
})

func sqlPackages(t *testing.T) map[string]int {
	t.Helper()

	found, err := scanned()
	must.NoError(t, err)

	return found
}

// moduleRoot is two directories up, which is where this package sits and where
// go.mod has to be for the answer to be this module rather than whatever tree a
// test binary was copied into.
func moduleRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	must.NoError(t, err)
	must.FileExists(t, filepath.Join(root, "go.mod"))

	return root
}

func scan(root string) (map[string]int, error) {
	found := map[string]int{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			// Dot directories hold no packages of this module's, and one of them
			// holds other checkouts of it: an agent worktree under .claude would
			// otherwise report every package in the module twice.
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "artifacts" || name == "testdata") {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		count, err := statementLiterals(path)
		if err != nil {
			return err
		}

		if count == 0 {
			return nil
		}

		dir, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}

		found[filepath.ToSlash(dir)] += count

		return nil
	})
	if err != nil {
		return nil, err
	}

	return found, nil
}

// statementLiterals counts the string literals in one file that open a
// statement. It reads the AST rather than the bytes so that a comment saying the
// word SELECT is not a statement, which is the false positive that put a package
// holding no SQL at all on a survey of the ones that do.
func statementLiterals(path string) (int, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return 0, err
	}

	var count int

	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}

		value, unquoteErr := strconv.Unquote(lit.Value)
		if unquoteErr != nil {
			return true
		}

		if sqlStatementOpening.MatchString(value) {
			count++
		}

		return true
	})

	return count, nil
}
