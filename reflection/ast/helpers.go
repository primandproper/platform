package ast

import (
	goast "go/ast"
	"go/types"
	"reflect"
	"strings"
)

// BuildImportMap returns a map from each import's local name (explicit alias or
// inferred last path segment) to its full import path. Blank ("_") and dot (".")
// imports are excluded.
//
// A dot import is excluded because it puts names into the file's scope under no
// qualifier at all, so a reference that resolves through one is indistinguishable
// from a reference to a type declared in the file's own package. There is nothing
// to map it to.
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

// ResolveImports maps each of a file's imports from its local name to the path
// it should be keyed under, given the module the file belongs to: a
// module-relative directory for one of that module's own packages ("." for the
// module root), and the unchanged import path for anything else.
//
// The two cannot collide, which is what makes the result usable as a single
// keyspace: a module-relative directory never begins with a domain name.
//
// Use FilterModuleImports instead when the external imports are genuinely not
// wanted; this keeps them, so a type reference into a dependency resolves rather
// than silently going missing.
func ResolveImports(file *goast.File, modulePath string) map[string]string {
	raw := BuildImportMap(file)
	resolved := make(map[string]string, len(raw))

	for localName, importPath := range raw {
		switch {
		case importPath == modulePath:
			resolved[localName] = "."
		case strings.HasPrefix(importPath, modulePath+"/"):
			resolved[localName] = strings.TrimPrefix(importPath, modulePath+"/")
		default:
			resolved[localName] = importPath
		}
	}

	return resolved
}

// FilterModuleImports filters an import map to only include module-internal imports
// and converts the values from full import paths to module-relative directory paths.
//
// Note that an import of the module root itself is dropped rather than mapped,
// since it has no relative directory below the root. ResolveImports is the
// variant that keeps everything and maps that case to ".".
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

// LookupTag reads a struct tag value whole, reporting whether the key was
// declared at all.
//
// The lookup is reflect.StructTag's rather than a scan of this package's own,
// because a struct tag is not a space-separated list: a value may itself contain
// spaces, and this repo writes several that do — `validate:"required,min=1"` is
// fine either way, but `env:"X" envDefault:"a b"` is not. Splitting on spaces
// read "a" as the whole default and then treated the orphaned `b"` as a further
// key, so the field after it in the tag went missing too. reflect.StructTag.Lookup
// implements the conventional grammar, quoting included, and is the definition
// the compiler and every reflection-based decoder already agree on.
//
// The value is returned uncut, and the reported bool distinguishes a declared
// empty value from an absent one. Both matter for tags whose grammar is not the
// conventional "value,option,option": an `envDefault` for a slice field is
// comma-separated all the way down, and declaring one empty is different from
// declaring none, since a default that exists always wins over a value some
// other layer supplied. GetTagValue is the narrower reading, for the tags that
// do follow the convention.
func LookupTag(tag, key string) (string, bool) {
	return reflect.StructTag(strings.Trim(tag, "`")).Lookup(key)
}

// GetTagValue extracts the value of a specific tag key from a raw struct field
// tag string (with or without surrounding backticks). It returns the value before
// any comma (i.e., omitting options like "omitempty"). Returns empty string if
// the key is not found.
//
// It is LookupTag narrowed to the conventional "value,option,option" grammar;
// see there for why the underlying parse is reflect.StructTag's.
func GetTagValue(tag, key string) string {
	value, ok := LookupTag(tag, key)
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
			if name := EmbeddedFieldName(field.Type); name != "" {
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

// EmbeddedFieldName derives the field name Go assigns to an embedded field from
// its type expression: the base type identifier, ignoring any leading pointer
// and any generic type arguments (e.g. "*pkg.Base[T]" is embedded as field
// "Base"). It returns "" for an expression that names no type.
//
// It is ParseTypeRef with the package qualifier dropped, which is what makes it
// the field's *name*: an embedded pkg.Base is reached as .Base, not as .pkg.Base.
func EmbeddedFieldName(expr goast.Expr) string {
	ref, ok := ParseTypeRef(expr)
	if !ok {
		return ""
	}

	return ref.Name
}
