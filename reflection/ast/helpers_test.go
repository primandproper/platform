package ast

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// structTypeFromSource parses a single struct declaration from source and returns its
// *ast.StructType, so tests can exercise realistic field types (pointers, slices, maps,
// generics, embedded fields) without hand-building the AST.
func structTypeFromSource(t *testing.T, src string) *ast.StructType {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), "src.go", "package p\n"+src, 0)
	must.NoError(t, err)

	gd, ok := f.Decls[0].(*ast.GenDecl)
	must.True(t, ok)
	ts, ok := gd.Specs[0].(*ast.TypeSpec)
	must.True(t, ok)
	st, ok := ts.Type.(*ast.StructType)
	must.True(t, ok)

	return st
}

// fileFromSource parses a whole file, for the helpers that read a file's
// imports rather than one declaration.
func fileFromSource(t *testing.T, src string) *ast.File {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), "src.go", src, parser.SkipObjectResolution)
	must.NoError(t, err)

	return f
}

func TestBuildImportMap(T *testing.T) {
	T.Parallel()

	T.Run("builds map from imports", func(t *testing.T) {
		t.Parallel()

		file := &ast.File{
			Imports: []*ast.ImportSpec{
				{Path: &ast.BasicLit{Value: `"fmt"`}},
				{Path: &ast.BasicLit{Value: `"github.com/example/pkg"`}},
			},
		}

		result := BuildImportMap(file)

		test.EqOp(t, "fmt", result["fmt"])
		test.EqOp(t, "github.com/example/pkg", result["pkg"])
	})

	T.Run("handles aliased imports", func(t *testing.T) {
		t.Parallel()

		file := &ast.File{
			Imports: []*ast.ImportSpec{
				{
					Name: &ast.Ident{Name: "myfmt"},
					Path: &ast.BasicLit{Value: `"fmt"`},
				},
			},
		}

		result := BuildImportMap(file)

		test.EqOp(t, "fmt", result["myfmt"])
	})

	T.Run("excludes blank and dot imports", func(t *testing.T) {
		t.Parallel()

		file := &ast.File{
			Imports: []*ast.ImportSpec{
				{
					Name: &ast.Ident{Name: "_"},
					Path: &ast.BasicLit{Value: `"image/png"`},
				},
				{
					Name: &ast.Ident{Name: "."},
					Path: &ast.BasicLit{Value: `"testing"`},
				},
			},
		}

		result := BuildImportMap(file)

		test.MapEmpty(t, result)
	})

	T.Run("skips imports with nil path", func(t *testing.T) {
		t.Parallel()

		file := &ast.File{
			Imports: []*ast.ImportSpec{
				{Path: nil},
			},
		}

		result := BuildImportMap(file)

		test.MapEmpty(t, result)
	})
}

func TestFilterModuleImports(T *testing.T) {
	T.Parallel()

	T.Run("filters to module-internal imports", func(t *testing.T) {
		t.Parallel()

		imports := map[string]string{
			"fmt":     "fmt",
			"logging": "github.com/example/mod/observability/logging",
			"errors":  "github.com/example/mod/errors",
		}

		result := FilterModuleImports(imports, "github.com/example/mod")

		test.MapLen(t, 2, result)
		test.EqOp(t, "observability/logging", result["logging"])
		test.EqOp(t, "errors", result["errors"])
	})

	T.Run("returns empty map when no module imports", func(t *testing.T) {
		t.Parallel()

		imports := map[string]string{
			"fmt": "fmt",
		}

		result := FilterModuleImports(imports, "github.com/example/mod")

		test.MapEmpty(t, result)
	})
}

func TestGetTagValue(T *testing.T) {
	T.Parallel()

	T.Run("extracts tag value", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "name", GetTagValue(`json:"name"`, "json"))
	})

	T.Run("extracts tag value with omitempty", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "name", GetTagValue(`json:"name,omitempty"`, "json"))
	})

	T.Run("extracts from multiple tags", func(t *testing.T) {
		t.Parallel()

		tag := `json:"name" env:"MY_VAR"`
		test.EqOp(t, "name", GetTagValue(tag, "json"))
		test.EqOp(t, "MY_VAR", GetTagValue(tag, "env"))
	})

	T.Run("returns empty for missing key", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", GetTagValue(`json:"name"`, "xml"))
	})

	T.Run("handles backtick-wrapped tags", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "name", GetTagValue("`json:\"name\"`", "json"))
	})

	T.Run("reads a value containing spaces whole", func(t *testing.T) {
		t.Parallel()

		// A struct tag is not a space-separated list. Splitting on spaces read
		// this default as "a" and then treated the orphaned `b"` as a key of its
		// own, which also swallowed the tag after it.
		tag := `env:"GREETING" envDefault:"hello there" json:"greeting"`

		test.EqOp(t, "GREETING", GetTagValue(tag, "env"))
		test.EqOp(t, "hello there", GetTagValue(tag, "envDefault"))
		test.EqOp(t, "greeting", GetTagValue(tag, "json"))
	})

	T.Run("returns empty for a malformed tag", func(t *testing.T) {
		t.Parallel()

		// An unterminated quote is not a tag reflect.StructTag can parse, and
		// guessing at what was meant is how the space-splitting version invented
		// values nobody wrote.
		test.EqOp(t, "", GetTagValue(`json:"name`, "json"))
	})
}

func TestGetStructFields(T *testing.T) {
	T.Parallel()

	T.Run("returns field names and types", func(t *testing.T) {
		t.Parallel()

		st := &ast.StructType{
			Fields: &ast.FieldList{
				List: []*ast.Field{
					{
						Names: []*ast.Ident{{Name: "Name"}},
						Type:  &ast.Ident{Name: "string"},
					},
					{
						Names: []*ast.Ident{{Name: "Logger"}},
						Type: &ast.SelectorExpr{
							X:   &ast.Ident{Name: "logging"},
							Sel: &ast.Ident{Name: "Logger"},
						},
					},
				},
			},
		}

		fields := GetStructFields(st)

		test.EqOp(t, "string", fields["Name"])
		test.EqOp(t, "logging.Logger", fields["Logger"])
	})

	T.Run("excludes underscore fields", func(t *testing.T) {
		t.Parallel()

		st := &ast.StructType{
			Fields: &ast.FieldList{
				List: []*ast.Field{
					{
						Names: []*ast.Ident{{Name: "_"}},
						Type:  &ast.Ident{Name: "int"},
					},
				},
			},
		}

		fields := GetStructFields(st)

		test.MapEmpty(t, fields)
	})

	T.Run("handles multiple names per field", func(t *testing.T) {
		t.Parallel()

		st := &ast.StructType{
			Fields: &ast.FieldList{
				List: []*ast.Field{
					{
						Names: []*ast.Ident{
							{Name: "X", NamePos: token.NoPos},
							{Name: "Y", NamePos: token.NoPos},
						},
						Type: &ast.Ident{Name: "int"},
					},
				},
			},
		}

		fields := GetStructFields(st)

		test.EqOp(t, "int", fields["X"])
		test.EqOp(t, "int", fields["Y"])
	})

	T.Run("renders pointer, slice, map, and generic field types", func(t *testing.T) {
		t.Parallel()

		st := structTypeFromSource(t, `type S struct {
			Ptr     *Foo
			Bytes   []byte
			Lookup  map[string]int
			Generic Box[string]
			Qual    time.Time
		}`)

		fields := GetStructFields(st)

		test.EqOp(t, "*Foo", fields["Ptr"])
		test.EqOp(t, "[]byte", fields["Bytes"])
		test.EqOp(t, "map[string]int", fields["Lookup"])
		test.EqOp(t, "Box[string]", fields["Generic"])
		test.EqOp(t, "time.Time", fields["Qual"])
	})

	T.Run("keys embedded fields by their base type name", func(t *testing.T) {
		t.Parallel()

		st := structTypeFromSource(t, `type S struct {
			BaseError
			*Embedded
			pkg.Qualified
			Field string
		}`)

		fields := GetStructFields(st)

		test.EqOp(t, "BaseError", fields["BaseError"])
		test.EqOp(t, "*Embedded", fields["Embedded"])
		test.EqOp(t, "pkg.Qualified", fields["Qualified"])
		test.EqOp(t, "string", fields["Field"])
	})
}

func TestResolveImports(T *testing.T) {
	T.Parallel()

	T.Run("keys this module's packages on their directory and everything else on its path", func(t *testing.T) {
		t.Parallel()

		file := fileFromSource(t, `package config

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
		}, ResolveImports(file, "example.com/app"))
	})
}

func TestLookupTag(T *testing.T) {
	T.Parallel()

	T.Run("returns the value whole, commas included", func(t *testing.T) {
		t.Parallel()

		value, ok := LookupTag("`envDefault:\"a,b,c\"`", "envDefault")

		must.True(t, ok)
		test.EqOp(t, "a,b,c", value)
	})

	T.Run("distinguishes a declared empty value from an absent one", func(t *testing.T) {
		t.Parallel()

		value, ok := LookupTag("`envDefault:\"\"`", "envDefault")
		must.True(t, ok)
		test.EqOp(t, "", value)

		_, ok = LookupTag("`env:\"X\"`", "envDefault")
		test.False(t, ok)
	})

	T.Run("reads a value containing spaces, which splitting on spaces would not", func(t *testing.T) {
		t.Parallel()

		value, ok := LookupTag("`env:\"X\" envDefault:\"a b\" json:\"x\"`", "envDefault")
		must.True(t, ok)
		test.EqOp(t, "a b", value)

		// The tag after the spaced value is still found, which is what a
		// space-split parse loses.
		value, ok = LookupTag("`env:\"X\" envDefault:\"a b\" json:\"x\"`", "json")
		must.True(t, ok)
		test.EqOp(t, "x", value)
	})
}
