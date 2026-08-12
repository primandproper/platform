// Package ast reads Go source as text: the helpers a code generator or an
// analysis tool needs to walk a repository's files and learn what is declared in
// them.
//
// It is the compile-time counterpart to the parent reflection package, and the
// distinction is which artifact each one inspects. reflection works on values
// and types in a running program, so it can only see code that is linked into
// the binary asking. This package works on parsed source, so it can describe a
// package the tool using it does not import and does not compile — which is the
// case a generator is always in.
//
// Nothing here runs on a request path. These are build-time tools, and they
// touch the filesystem: GetModulePath reads a go.mod to answer what a directory's
// module is called, which is what makes an import path classifiable as
// module-internal or third-party.
//
// The type of a struct field is reported as the Go source that spells it —
// "*pkg.T", "map[string]int", "Foo[T]" — rather than as a resolved type, because
// resolution needs a type checker and a build, and a generator reading one file
// has neither. An embedded field is keyed under the name Go gives it: the base
// identifier, with any pointer and type arguments stripped.
//
// Struct tags are read with reflect.StructTag rather than by splitting on
// spaces. A tag is not a space-separated list — a value may contain spaces, and
// this repository writes several that do — so the conventional grammar, quoting
// included, is the only parse that agrees with what the compiler and every
// reflection-based decoder see.
package ast

import (
	"bufio"
	goast "go/ast"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/primandproper/platform-go/v10/errors"
)

// GetModulePath reads the module path from the go.mod file in the given directory.
func GetModulePath(dir string) (string, error) {
	f, err := os.Open(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", errors.Wrap(err, "opening go.mod")
	}
	defer func() {
		_ = f.Close() //nolint:errcheck // read-only file; close error is not actionable here
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(after), nil
		}
	}

	if err = scanner.Err(); err != nil {
		return "", errors.Wrap(err, "scanning go.mod")
	}

	return "", errors.New("no module directive found in go.mod")
}

// BuildImportMap returns a map from each import's local name (explicit alias or
// inferred last path segment) to its full import path. Blank ("_") and dot (".")
// imports are excluded.
func BuildImportMap(file *goast.File) map[string]string {
	result := make(map[string]string)

	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}

		importPath := strings.Trim(imp.Path.Value, `"`)

		var localName string
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				continue
			}
			localName = imp.Name.Name
		} else {
			parts := strings.Split(importPath, "/")
			localName = parts[len(parts)-1]
		}

		result[localName] = importPath
	}

	return result
}

// FilterModuleImports filters an import map to only include module-internal imports
// and converts the values from full import paths to module-relative directory paths.
func FilterModuleImports(imports map[string]string, modulePath string) map[string]string {
	result := make(map[string]string)
	prefix := modulePath + "/"

	for localName, importPath := range imports {
		if after, ok := strings.CutPrefix(importPath, prefix); ok {
			result[localName] = after
		}
	}

	return result
}

// GetTagValue extracts the value of a specific tag key from a raw struct field
// tag string (with or without surrounding backticks). It returns the value before
// any comma (i.e., omitting options like "omitempty"). Returns empty string if
// the key is not found.
//
// The lookup is reflect.StructTag's rather than a scan of this package's own,
// because a struct tag is not a space-separated list: a value may itself contain
// spaces, and this repo writes several that do — `validate:"required,min=1"` is
// fine either way, but `env:"X" envDefault:"a b"` is not. Splitting on spaces
// read "a" as the whole default and then treated the orphaned `b"` as a further
// key, so the field after it in the tag went missing too. reflect.StructTag.Lookup
// implements the conventional grammar, quoting included, and is the definition
// the compiler and every reflection-based decoder already agree on.
func GetTagValue(tag, key string) string {
	value, ok := reflect.StructTag(strings.Trim(tag, "`")).Lookup(key)
	if !ok {
		return ""
	}

	before, _, _ := strings.Cut(value, ",")

	return before
}

// GetStructFields returns a map of field names to their type representation
// from an *ast.StructType. Fields named "_" are excluded.
//
// The type representation is the field type rendered as Go source, so all field
// kinds are handled: "TypeName" (local), "pkg.TypeName" (imported), "*T" (pointer),
// "[]byte" (slice/array), "map[string]int" (map), "Foo[T]" (generic), and so on.
// Embedded (anonymous) fields are keyed by the embedded type's base name — e.g. an
// embedded "pkg.Base" or "*pkg.Base" is keyed "Base". An embedded field whose name
// cannot be derived (rare, e.g. an anonymous instantiated type with no resolvable
// base ident) is skipped.
func GetStructFields(structType *goast.StructType) map[string]string {
	fields := make(map[string]string)

	for _, field := range structType.Fields.List {
		fieldType := types.ExprString(field.Type)

		if len(field.Names) == 0 {
			// Embedded/anonymous field: derive the name from the type itself.
			if name := embeddedFieldName(field.Type); name != "" {
				fields[name] = fieldType
			}
			continue
		}

		for _, name := range field.Names {
			if name.Name != "_" {
				fields[name.Name] = fieldType
			}
		}
	}

	return fields
}

// embeddedFieldName derives the field name Go assigns to an embedded field from its
// type expression: the base type identifier, ignoring any leading pointer and any
// generic type arguments (e.g. "*pkg.Base[T]" is embedded as field "Base").
func embeddedFieldName(expr goast.Expr) string {
	switch t := expr.(type) {
	case *goast.StarExpr:
		return embeddedFieldName(t.X)
	case *goast.Ident:
		return t.Name
	case *goast.SelectorExpr:
		return t.Sel.Name
	case *goast.IndexExpr:
		return embeddedFieldName(t.X)
	case *goast.IndexListExpr:
		return embeddedFieldName(t.X)
	default:
		return ""
	}
}
