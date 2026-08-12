package catalogen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/primandproper/platform-go/v10/webhooks"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The literal render emits, compiled. A generator's output is checked by
// whoever runs it, not by this module's build, so the one thing this module can
// still assert is that the shape it writes is the shape webhooks accepts: if
// EventDefinition grows a field the elided literal must set, or Catalog stops
// being a map keyed by event type, this stops building.
var _ = webhooks.Catalog{
	"order.created": {Description: "Fires when an order is placed."},
}

func TestSourceDir(T *testing.T) {
	T.Parallel()

	T.Run("a relative directory is written as given", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "internal/domain", sourceDir("internal/domain"))
		test.EqOp(t, "internal/domain", sourceDir("./internal/domain/"))
	})

	T.Run("a directory under the working directory is relativized", func(t *testing.T) {
		t.Parallel()

		// Otherwise the checked-in file names whoever's home directory last
		// generated it, and every other developer's run rewrites it.
		cwd, err := os.Getwd()
		must.NoError(t, err)

		test.EqOp(t, "webhooks/catalogen", sourceDir(filepath.Join(cwd, "webhooks", "catalogen")))
	})

	T.Run("a directory outside the working directory is left absolute", func(t *testing.T) {
		t.Parallel()

		elsewhere := filepath.Join(t.TempDir(), "domain")

		test.EqOp(t, filepath.ToSlash(elsewhere), sourceDir(elsewhere))
	})
}

func TestParseCatalog(T *testing.T) {
	T.Parallel()

	T.Run("reads back what render wrote", func(t *testing.T) {
		t.Parallel()

		src, err := render(&Options{Suffix: defaultSuffix, Dir: "domain", Package: "catalog", VarName: defaultVarName}, []entry{
			{constName: "OrderCreatedEventType", eventType: "order.created", description: "Fires when an order is placed."},
			{constName: "QuotedEventType", eventType: "order.\"quoted\"", description: `Contains "quotes" and a \backslash.`},
		})
		must.NoError(t, err)

		catalog, ok := parseCatalog(src, defaultVarName)
		must.True(t, ok)
		test.Eq(t, map[string]string{
			"order.created":  "Fires when an order is placed.",
			`order."quoted"`: `Contains "quotes" and a \backslash.`,
		}, catalog)
	})

	T.Run("declines what it cannot read as a catalog", func(t *testing.T) {
		t.Parallel()

		for name, src := range map[string]string{
			"not Go":            "this is not Go at all",
			"no such variable":  "package catalog\n\nvar Other = webhooks.Catalog{}\n",
			"not a literal":     "package catalog\n\nvar Catalog = buildCatalog()\n",
			"unreadable key":    "package catalog\n\nvar Catalog = webhooks.Catalog{someKey: {}}\n",
			"unreadable value":  "package catalog\n\nvar Catalog = webhooks.Catalog{\"a\": someDefinition}\n",
			"not a keyed entry": "package catalog\n\nvar Catalog = webhooks.Catalog{{}}\n",
		} {
			_, ok := parseCatalog([]byte(src), defaultVarName)
			test.False(t, ok, test.Sprintf("source %q", name))
		}
	})

	T.Run("an entry with no description reads as empty prose", func(t *testing.T) {
		t.Parallel()

		catalog, ok := parseCatalog([]byte("package catalog\n\nvar Catalog = webhooks.Catalog{\"a\": {}, \"b\": {Other: \"x\"}}\n"), defaultVarName)
		must.True(t, ok)
		test.Eq(t, map[string]string{"a": "", "b": ""}, catalog)
	})
}

func TestSummarize(T *testing.T) {
	T.Parallel()

	T.Run("says nothing about an empty category", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", summarize("missing", nil))
	})

	T.Run("sorts so the message is stable", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "2 missing (a.event, b.event)", summarize("missing", []string{"b.event", "a.event"}))
	})

	T.Run("truncates a wholesale change", func(t *testing.T) {
		t.Parallel()

		// A catalog that grew by two hundred events has one cause, and listing
		// all of them buries it.
		events := []string{"m", "l", "k", "j", "i", "h", "g", "f", "e", "d", "c", "b", "a"}

		summary := summarize("missing", events)

		test.EqOp(t, "13 missing (a, b, c, d, e, f, g, h and 5 more)", summary)
	})
}
