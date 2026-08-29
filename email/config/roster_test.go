package emailcfg

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// rosterLine matches one entry of the indented provider block in email's
// package doc: a tab, the provider's name, and the prose describing it.
var rosterLine = regexp.MustCompile(`^\t([a-z0-9]+) {2,}\S`)

// TestDocRosterMatchesProviders checks the provider list in email's package doc
// against the roster this package dispatches on.
//
// The two are a copy of each other, and a copy of a list is the kind of
// documentation that goes quietly wrong: a vendor is added here, the doc goes
// on naming five of the seven, and nothing says so until somebody reads both.
// That is not hypothetical — it is the drift this test was written to end.
//
// It lives in this package rather than in email because providers is
// unexported, and exporting a roster so a test in the neighboring package
// could read it would widen the API for the test's convenience. The doc it
// parses is the one belonging to the package this one configures, which is the
// direction the dependency already runs.
func TestDocRosterMatchesProviders(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()

	parsed, err := parser.ParseFile(fset, filepath.Join("..", "doc.go"), nil, parser.ParseComments)
	must.NoError(t, err)
	must.NotNil(t, parsed.Doc)

	var documented []string

	for line := range strings.Lines(parsed.Doc.Text()) {
		if match := rosterLine.FindStringSubmatch(strings.TrimRight(line, "\n")); match != nil {
			documented = append(documented, match[1])
		}
	}

	must.SliceNotEmpty(t, documented)

	expected := slices.Clone(providers)
	slices.Sort(expected)
	slices.Sort(documented)

	test.Eq(t, expected, documented, test.Sprintf(
		"email/doc.go names %v; emailcfg dispatches on %v", documented, expected,
	))
}
