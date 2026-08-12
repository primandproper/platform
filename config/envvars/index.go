package envvars

import (
	goast "go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/primandproper/platform-go/v10/errors"
	reflast "github.com/primandproper/platform-go/v10/reflection/ast"
)

// structEntry is a struct declaration together with what resolving the types
// its fields name requires: the key of the package it was declared in, for
// unqualified references, and the imports of the file it was declared in, for
// qualified ones.
type structEntry struct {
	structType *goast.StructType
	imports    map[string]string
	pkgKey     string
	name       string
}

func (e *structEntry) key() string {
	return e.pkgKey + "." + e.name
}

// index is every struct declaration and every type union found in the modules
// that were parsed, keyed by "<package>.<TypeName>".
//
// A package's key is its module-relative directory for the module being
// generated for, and its full import path for a dependency. The two cannot
// collide: a module-relative directory never begins with a domain name.
type index struct {
	structs map[string]*structEntry
	unions  map[string][]string
}

func newIndex() *index {
	return &index{
		structs: map[string]*structEntry{},
		unions:  map[string][]string{},
	}
}

// parseModule walks the module rooted at dir and records everything it
// declares. pkgPrefix is the key prefix for its packages: empty for the module
// being generated for, whose packages key on their module-relative directory,
// and the module's import path for a dependency. modulePath is the path of the
// module being generated for, which is what makes an import classifiable as one
// of its own.
func (idx *index) parseModule(dir, pkgPrefix, modulePath string) error {
	root, err := filepath.Abs(dir)
	if err != nil {
		return errors.Wrapf(err, "resolving module directory %q", dir)
	}

	fset := token.NewFileSet()

	if err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if path != root && skipDir(path, entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return errors.Wrapf(parseErr, "parsing %q", path)
		}

		rel, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil {
			return errors.Wrapf(relErr, "locating %q within %q", path, root)
		}

		idx.addFile(file, packageKey(pkgPrefix, rel), modulePath)

		return nil
	}); err != nil {
		return errors.Wrapf(err, "walking module directory %q", dir)
	}

	return nil
}

// skipDir reports whether a directory encountered during the walk holds source
// that is not this module's. Test fixtures and vendored copies are not, and
// neither is a nested module: it has its own go.mod, and its types belong to
// whatever import path that names rather than to a directory under this one.
func skipDir(path, name string) bool {
	if name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}

	_, err := os.Stat(filepath.Join(path, "go.mod"))

	return err == nil
}

// packageKey builds the index key prefix for one package.
func packageKey(pkgPrefix, relDir string) string {
	relDir = filepath.ToSlash(relDir)

	switch {
	case pkgPrefix == "":
		return relDir
	case relDir == ".":
		return pkgPrefix
	default:
		return pkgPrefix + "/" + relDir
	}
}

// addFile records the structs and type unions one file declares.
func (idx *index) addFile(file *goast.File, pkgKey, modulePath string) {
	imports := packageImports(file, modulePath)

	for _, decl := range file.Decls {
		genDecl, isGenDecl := decl.(*goast.GenDecl)
		if !isGenDecl || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, isTypeSpec := spec.(*goast.TypeSpec)
			if !isTypeSpec {
				continue
			}

			key := pkgKey + "." + typeSpec.Name.Name

			switch declared := typeSpec.Type.(type) {
			case *goast.StructType:
				idx.structs[key] = &structEntry{structType: declared, imports: imports, pkgKey: pkgKey, name: typeSpec.Name.Name}
			case *goast.InterfaceType:
				if members := unionMembers(declared, pkgKey, imports); len(members) > 0 {
					idx.unions[key] = members
				}
			}
		}
	}
}

// packageImports maps each import's local name to the index key of the package
// it names: a module-relative directory for one of this module's own packages,
// and the import path itself for anything else.
func packageImports(file *goast.File, modulePath string) map[string]string {
	raw := reflast.BuildImportMap(file)
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

// unionMembers returns the index keys of the members of an interface that is a
// pure type union (`interface{ A | B | pkg.C }`), in declaration order. It
// returns nil for a method-bearing or otherwise non-union interface.
func unionMembers(iface *goast.InterfaceType, pkgKey string, imports map[string]string) []string {
	var members []string

	for _, field := range iface.Methods.List {
		terms := unionTerms(field.Type)
		if terms == nil {
			return nil
		}

		for i := range terms {
			key, ok := terms[i].key(pkgKey, imports)
			if !ok {
				return nil
			}

			members = append(members, key)
		}
	}

	return members
}

// unionTerms flattens an `A | ~B | pkg.C` constraint expression into the types
// it names.
func unionTerms(expr goast.Expr) []typeRef {
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
		if ref, ok := parseTypeRef(expr); ok {
			return []typeRef{ref}
		}

		return nil
	}
}

// typeRef is a named type as a struct field or a union term writes it: a type
// name, qualified by the local name of the package it was imported under when
// it comes from elsewhere.
type typeRef struct {
	pkgLocalName string
	name         string
}

// key resolves the reference to an index key, given the key of the package the
// reference was written in and that file's imports. It reports false for a
// qualified reference whose package was not imported by the file, which is what
// a dot import or a reference this package cannot see looks like from here.
func (r typeRef) key(pkgKey string, imports map[string]string) (string, bool) {
	if r.pkgLocalName == "" {
		return pkgKey + "." + r.name, true
	}

	importKey, found := imports[r.pkgLocalName]
	if !found {
		return "", false
	}

	return importKey + "." + r.name, true
}

// parseTypeRef reads the named type out of a type expression, seeing through
// the wrappers that do not change which type is named: parentheses, a pointer,
// and generic type arguments. It reports false for anything else — a slice, a
// map, a function type, an inline struct — none of which names a type this
// package can look up.
func parseTypeRef(expr goast.Expr) (typeRef, bool) {
	switch node := expr.(type) {
	case *goast.ParenExpr:
		return parseTypeRef(node.X)
	case *goast.StarExpr:
		return parseTypeRef(node.X)
	case *goast.IndexExpr:
		return parseTypeRef(node.X)
	case *goast.IndexListExpr:
		return parseTypeRef(node.X)
	case *goast.Ident:
		return typeRef{name: node.Name}, true
	case *goast.SelectorExpr:
		pkgIdent, isIdent := node.X.(*goast.Ident)
		if !isIdent {
			return typeRef{}, false
		}

		return typeRef{pkgLocalName: pkgIdent.Name, name: node.Sel.Name}, true
	default:
		return typeRef{}, false
	}
}

// inlineStruct returns the struct literal a field declares inline, seeing
// through the same wrappers parseTypeRef does.
func inlineStruct(expr goast.Expr) (*goast.StructType, bool) {
	switch node := expr.(type) {
	case *goast.ParenExpr:
		return inlineStruct(node.X)
	case *goast.StarExpr:
		return inlineStruct(node.X)
	case *goast.StructType:
		return node, true
	default:
		return nil, false
	}
}

// roots returns the index keys of the configuration structs to walk, in the
// order they were declared.
func (idx *index) roots(opts *Options) ([]string, error) {
	keys := opts.Roots

	if opts.UnionKey != "" {
		members, found := idx.unions[opts.UnionKey]
		if !found {
			return nil, errors.Newf("no type union named %q was found; it is what UnionKey must name", opts.UnionKey)
		}

		keys = members
	}

	for _, key := range keys {
		if _, found := idx.structs[key]; !found {
			return nil, errors.Newf("configuration struct %q was named but not found by the parser", key)
		}
	}

	return keys, nil
}
