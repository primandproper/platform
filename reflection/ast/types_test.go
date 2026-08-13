package ast

import (
	goast "go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// typeExprFromSource parses `type X <expr>` and returns the type expression, so
// tests can write the type the way source does.
func typeExprFromSource(t *testing.T, expr string) goast.Expr {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), "src.go", "package p\n\ntype X "+expr+"\n", parser.SkipObjectResolution)
	must.NoError(t, err)

	gd, ok := f.Decls[0].(*goast.GenDecl)
	must.True(t, ok)
	ts, ok := gd.Specs[0].(*goast.TypeSpec)
	must.True(t, ok)

	return ts.Type
}

// interfaceFromSource parses a single interface declaration and returns it.
func interfaceFromSource(t *testing.T, body string) *goast.InterfaceType {
	t.Helper()

	iface, ok := typeExprFromSource(t, "interface{"+body+"}").(*goast.InterfaceType)
	must.True(t, ok)

	return iface
}

func TestParseTypeRef(T *testing.T) {
	T.Parallel()

	T.Run("reads an unqualified name", func(t *testing.T) {
		t.Parallel()

		ref, ok := ParseTypeRef(typeExprFromSource(t, "Config"))

		must.True(t, ok)
		test.EqOp(t, TypeRef{Name: "Config"}, ref)
	})

	T.Run("reads a qualified name", func(t *testing.T) {
		t.Parallel()

		ref, ok := ParseTypeRef(typeExprFromSource(t, "database.Config"))

		must.True(t, ok)
		test.EqOp(t, TypeRef{Package: "database", Name: "Config"}, ref)
	})

	T.Run("sees through the wrappers that do not change which type is named", func(t *testing.T) {
		t.Parallel()

		for _, expr := range []string{"*database.Config", "(database.Config)", "*(database.Config)", "database.Config[int]", "database.Config[int, string]", "*database.Config[int]"} {
			ref, ok := ParseTypeRef(typeExprFromSource(t, expr))

			must.True(t, ok, must.Sprintf("parsing %q", expr))
			test.EqOp(t, TypeRef{Package: "database", Name: "Config"}, ref, test.Sprintf("parsing %q", expr))
		}
	})

	T.Run("reports an expression that names no single type", func(t *testing.T) {
		t.Parallel()

		for _, expr := range []string{"[]byte", "map[string]int", "func() error", "struct{ A int }", "chan int", "[4]string"} {
			_, ok := ParseTypeRef(typeExprFromSource(t, expr))

			test.False(t, ok, test.Sprintf("parsing %q", expr))
		}
	})
}

func TestEmbeddedFieldName(T *testing.T) {
	T.Parallel()

	T.Run("drops the package qualifier, which is not part of the field's name", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "Base", EmbeddedFieldName(typeExprFromSource(t, "*pkg.Base[T]")))
		test.EqOp(t, "Base", EmbeddedFieldName(typeExprFromSource(t, "Base")))
	})

	T.Run("reports nothing for an expression that names no type", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", EmbeddedFieldName(typeExprFromSource(t, "map[string]int")))
	})
}

func TestInlineStruct(T *testing.T) {
	T.Parallel()

	T.Run("returns the struct a field declares inline", func(t *testing.T) {
		t.Parallel()

		for _, expr := range []string{"struct{ A int }", "*struct{ A int }", "(struct{ A int })"} {
			structType, ok := InlineStruct(typeExprFromSource(t, expr))

			must.True(t, ok, must.Sprintf("parsing %q", expr))
			test.SliceLen(t, 1, structType.Fields.List)
		}
	})

	T.Run("reports a named type, which has no struct literal in it", func(t *testing.T) {
		t.Parallel()

		_, ok := InlineStruct(typeExprFromSource(t, "database.Config"))

		test.False(t, ok)
	})
}

func TestUnionTerms(T *testing.T) {
	T.Parallel()

	T.Run("flattens the types a union names, in declaration order", func(t *testing.T) {
		t.Parallel()

		terms, ok := UnionTerms(interfaceFromSource(t, "A | B | database.Config"))

		must.True(t, ok)
		test.Eq(t, []TypeRef{{Name: "A"}, {Name: "B"}, {Package: "database", Name: "Config"}}, terms)
	})

	T.Run("discards the tilde on an approximate term", func(t *testing.T) {
		t.Parallel()

		terms, ok := UnionTerms(interfaceFromSource(t, "~A | ~B"))

		must.True(t, ok)
		test.Eq(t, []TypeRef{{Name: "A"}, {Name: "B"}}, terms)
	})

	T.Run("collects terms across several constraint lines", func(t *testing.T) {
		t.Parallel()

		terms, ok := UnionTerms(interfaceFromSource(t, "\nA | B\nC\n"))

		must.True(t, ok)
		test.Eq(t, []TypeRef{{Name: "A"}, {Name: "B"}, {Name: "C"}}, terms)
	})

	T.Run("reports an interface carrying a method", func(t *testing.T) {
		t.Parallel()

		_, ok := UnionTerms(interfaceFromSource(t, "Do() error"))

		test.False(t, ok)
	})

	T.Run("reports a union of types it cannot name", func(t *testing.T) {
		t.Parallel()

		_, ok := UnionTerms(interfaceFromSource(t, "[]byte | map[string]int"))

		test.False(t, ok)
	})

	T.Run("reports the empty interface, which constrains nothing", func(t *testing.T) {
		t.Parallel()

		_, ok := UnionTerms(interfaceFromSource(t, ""))

		test.False(t, ok)
	})
}
