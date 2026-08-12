package ast

import (
	goast "go/ast"
	"go/token"
)

// TypeRef is a named type as source writes it: a type name, qualified by the
// local name of the package it was imported under when it comes from elsewhere.
//
// It is deliberately the *written* reference rather than a resolved type. A
// generator reading one file has no type checker and no build, so the local
// name is all it has; turning that into something canonical needs the file's
// imports, which is the caller's to supply because only the caller knows what
// it wants to key on.
type TypeRef struct {
	// Package is the local name the qualifying package was imported under, as
	// written. It is empty for a reference to a type in the same package.
	Package string

	// Name is the type's own name, unqualified.
	Name string
}

// ParseTypeRef reads the named type out of a type expression, seeing through
// the wrappers that do not change which type is named: parentheses, a pointer,
// and generic type arguments.
//
// It reports false for anything that does not name a single type — a slice, a
// map, a function type, an inline struct — because there is no name in it to
// resolve. Use InlineStruct for the struct-literal case, which is the one that
// has fields to walk despite naming nothing.
func ParseTypeRef(expr goast.Expr) (TypeRef, bool) {
	switch node := expr.(type) {
	case *goast.ParenExpr:
		return ParseTypeRef(node.X)
	case *goast.StarExpr:
		return ParseTypeRef(node.X)
	case *goast.IndexExpr:
		return ParseTypeRef(node.X)
	case *goast.IndexListExpr:
		return ParseTypeRef(node.X)
	case *goast.Ident:
		return TypeRef{Name: node.Name}, true
	case *goast.SelectorExpr:
		pkgIdent, isIdent := node.X.(*goast.Ident)
		if !isIdent {
			return TypeRef{}, false
		}

		return TypeRef{Package: pkgIdent.Name, Name: node.Sel.Name}, true
	default:
		return TypeRef{}, false
	}
}

// InlineStruct returns the struct literal a type expression declares inline,
// seeing through the same parenthesis and pointer wrappers ParseTypeRef does.
//
// An inline struct is the case that names no type and still has fields worth
// walking, so a caller that recurses into struct-typed fields has to ask this
// as well as ParseTypeRef.
func InlineStruct(expr goast.Expr) (*goast.StructType, bool) {
	switch node := expr.(type) {
	case *goast.ParenExpr:
		return InlineStruct(node.X)
	case *goast.StarExpr:
		return InlineStruct(node.X)
	case *goast.StructType:
		return node, true
	default:
		return nil, false
	}
}

// UnionTerms returns the types named by an interface that is a pure type union
// — `interface{ A | ~B | pkg.C }` — in declaration order.
//
// It reports false for anything else: an interface carrying a method, one whose
// terms include a type this cannot name (a slice, a map), and the empty
// interface, which constrains nothing and so has no members to enumerate.
//
// The tilde is discarded. `~B` and `B` name the same type for the purpose of
// asking what a constraint's members are; the difference is whether types
// *defined* as B also satisfy it, which is not a question the source can answer
// without resolving every type in the module.
func UnionTerms(iface *goast.InterfaceType) ([]TypeRef, bool) {
	var terms []TypeRef

	for _, field := range iface.Methods.List {
		fieldTerms := unionTerms(field.Type)
		if fieldTerms == nil {
			return nil, false
		}

		terms = append(terms, fieldTerms...)
	}

	if len(terms) == 0 {
		return nil, false
	}

	return terms, true
}

// unionTerms flattens one `A | ~B | pkg.C` constraint expression into the types
// it names, reporting nil for an expression that is not a union of nameable
// types.
func unionTerms(expr goast.Expr) []TypeRef {
	switch node := expr.(type) {
	case *goast.BinaryExpr:
		if node.Op != token.OR {
			return nil
		}

		left, right := unionTerms(node.X), unionTerms(node.Y)
		if left == nil || right == nil {
			return nil
		}

		return append(left, right...)
	case *goast.UnaryExpr:
		if node.Op != token.TILDE {
			return nil
		}

		return unionTerms(node.X)
	default:
		if ref, ok := ParseTypeRef(expr); ok {
			return []TypeRef{ref}
		}

		return nil
	}
}
