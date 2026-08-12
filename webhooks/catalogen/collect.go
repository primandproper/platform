package catalogen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

// entry is one event type found in the scanned tree: what the constant is
// called, what it holds, and what its doc comment says about it.
type entry struct {
	// constName is the constant's name. It is carried into the generated file
	// as a comment beside the entry, which is what turns an event type read in
	// the catalog back into the declaration it came from.
	constName string

	// eventType is the constant's value, and the catalog's key.
	eventType string

	// description is the doc comment, normalized for a UI to render.
	description string

	// path is the file the constant was declared in, used only to make a
	// duplicate or a non-string value point at somewhere to look.
	path string
}

// collect walks opts.Dir and returns every event type constant under it, sorted
// by event type so that generation is deterministic.
func collect(opts *Options) ([]entry, error) {
	output, err := filepath.Abs(opts.OutputPath)
	if err != nil {
		return nil, platformerrors.Wrapf(err, "resolving output path %s", opts.OutputPath)
	}

	fset := token.NewFileSet()

	var entries []entry

	// Where each event type was already found, so a duplicate can name the
	// constant that claimed it first.
	seen := map[string]int{}

	walkErr := filepath.WalkDir(opts.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if path != opts.Dir && skipDir(d.Name()) {
				return fs.SkipDir
			}

			return nil
		}

		if !isScannableFile(path) {
			return nil
		}

		// The generated catalog frequently lives under the tree it is generated
		// from, and skipping it keeps a regeneration from reading its own
		// previous output.
		if abs, absErr := filepath.Abs(path); absErr == nil && abs == output {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if parseErr != nil {
			return platformerrors.Wrapf(parseErr, "parsing %s", path)
		}

		found, constErr := constantsIn(file, path, opts.Suffix)
		if constErr != nil {
			return constErr
		}

		for i := range found {
			e := &found[i]

			if at, ok := seen[e.eventType]; ok {
				existing := &entries[at]

				return platformerrors.Wrapf(
					ErrDuplicateEventType,
					"%q is declared by %s in %s and by %s in %s",
					e.eventType, existing.constName, existing.path, e.constName, e.path,
				)
			}

			seen[e.eventType] = len(entries)
			entries = append(entries, *e)
		}

		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	if len(entries) == 0 {
		return nil, platformerrors.Wrapf(ErrNoEventTypes, "no constants ending in %q under %s", opts.Suffix, opts.Dir)
	}

	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.eventType, b.eventType) })

	return entries, nil
}

// skipDir reports whether a directory is one no event type can legitimately be
// declared in: dependencies, test fixtures, and the tool directories Go itself
// ignores.
func skipDir(name string) bool {
	return name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// isScannableFile reports whether a file is Go source that ships. Test files are
// excluded: a constant declared in one is not published by the application, and
// collecting it would put an event in the catalog that nothing can dispatch.
func isScannableFile(path string) bool {
	return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
}

// constantsIn returns the event type constants declared in one parsed file.
func constantsIn(file *ast.File, path, suffix string) ([]entry, error) {
	var entries []entry

	for _, decl := range file.Decls {
		gen, isGenDecl := decl.(*ast.GenDecl)
		if !isGenDecl || gen.Tok != token.CONST {
			continue
		}

		for _, spec := range gen.Specs {
			valueSpec, isValueSpec := spec.(*ast.ValueSpec)
			if !isValueSpec {
				continue
			}

			for i, name := range valueSpec.Names {
				if name.Name == "_" || !strings.HasSuffix(name.Name, suffix) {
					continue
				}

				e, err := entryFor(gen, valueSpec, i, name.Name, path)
				if err != nil {
					return nil, err
				}

				entries = append(entries, e)
			}
		}
	}

	return entries, nil
}

// entryFor builds the entry for the i-th name in a value spec.
func entryFor(gen *ast.GenDecl, valueSpec *ast.ValueSpec, i int, name, path string) (entry, error) {
	if i >= len(valueSpec.Values) {
		// A name with no value of its own: an iota member, or a repetition of
		// the previous spec's expression.
		return entry{}, platformerrors.Wrapf(ErrNotAStringConstant, "%s in %s has no value of its own", name, path)
	}

	value, ok := stringValue(valueSpec.Values[i])
	if !ok {
		return entry{}, platformerrors.Wrapf(ErrNotAStringConstant, "%s in %s", name, path)
	}

	if value == "" {
		return entry{}, platformerrors.Wrapf(ErrEmptyEventType, "%s in %s", name, path)
	}

	return entry{
		constName:   name,
		eventType:   value,
		description: describe(docFor(gen, valueSpec), name),
		path:        path,
	}, nil
}

// stringValue reads the string a constant's expression evaluates to, for the
// forms an event type is written in: a bare literal, a conversion to the
// application's own event type, and either of those parenthesized. Anything
// else — an identifier, a concatenation, an iota — is not resolvable without a
// type checker and is reported rather than guessed at.
func stringValue(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}

		value, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}

		return value, true
	case *ast.CallExpr:
		if len(e.Args) != 1 {
			return "", false
		}

		return stringValue(e.Args[0])
	case *ast.ParenExpr:
		return stringValue(e.X)
	default:
		return "", false
	}
}

// docFor returns the comment group documenting a constant. A spec's own doc
// comment wins; a single-spec const declaration carries its comment on the
// declaration instead, and a trailing line comment is the last resort.
func docFor(gen *ast.GenDecl, valueSpec *ast.ValueSpec) *ast.CommentGroup {
	switch {
	case valueSpec.Doc != nil:
		return valueSpec.Doc
	case gen.Doc != nil && len(gen.Specs) == 1:
		return gen.Doc
	default:
		return valueSpec.Comment
	}
}

// describe turns a doc comment into the prose a subscription UI renders beside
// the checkbox: one line, with the constant's own name trimmed off the front so
// the reader is not shown a Go identifier, and the result capitalized so what
// remains reads as a sentence.
func describe(doc *ast.CommentGroup, name string) string {
	if doc == nil {
		return ""
	}

	// CommentGroup.Text strips the markers and drops directive comments, and
	// Fields collapses the line breaks a wrapped comment arrives with.
	text := strings.Join(strings.Fields(doc.Text()), " ")

	switch after, ok := strings.CutPrefix(text, name+" "); {
	case ok:
		text = after
	case text == name:
		// A comment that is only the identifier — the shape a doc-comment lint
		// rule is satisfied by and a reader learns nothing from. Trimming it to
		// nothing keeps a Go identifier out of the subscription UI.
		text = ""
	}

	return capitalize(text)
}

// capitalize upper-cases the first rune, which is what makes a comment written
// as a continuation of its identifier ("fires when an order is placed") read as
// a sentence once the identifier is gone.
func capitalize(text string) string {
	if text == "" {
		return ""
	}

	runes := []rune(text)
	runes[0] = unicode.ToUpper(runes[0])

	return string(runes)
}
