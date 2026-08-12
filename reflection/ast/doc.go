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
// Nothing here runs on a request path. These are build-time tools: they touch
// the filesystem, and ModuleDirs runs `go list` when there is no vendor
// directory to read instead.
//
// # What it answers
//
// Where source lives: GetModulePath reads a go.mod to answer what a directory's
// module is called, which is what makes an import path classifiable as
// module-internal or third-party. ModuleDirs finds the source of everything that
// module depends on. WalkModule parses the files that belong to one module and
// nothing else — the exclusions it encodes (vendor, testdata, nested go.mod) are
// the part that is easy to get subtly wrong.
//
// What a file declares: BuildImportMap and ResolveImports turn a file's imports
// into the lookup a type reference has to be resolved through. GetStructFields
// reports a struct's fields, ParseTypeRef and InlineStruct read what a field's
// type expression names, and UnionTerms enumerates the members of a type-union
// constraint — which is how a generator can key on a constraint rather than on a
// hand-kept list and be complete by construction.
//
// # Names, not resolved types
//
// The type of a struct field is reported as the Go source that spells it —
// "*pkg.T", "map[string]int", "Foo[T]" — and a type reference keeps the local
// name of the package it was written under, rather than either being resolved.
// Resolution needs a type checker and a build, and a generator reading one file
// has neither. Turning a reference into something canonical needs the declaring
// file's imports, which is why that step is the caller's: only the caller knows
// what it wants to key on.
//
// An embedded field is keyed under the name Go gives it: the base identifier,
// with any pointer and type arguments stripped.
//
// # Struct tags
//
// Struct tags are read with reflect.StructTag rather than by splitting on
// spaces. A tag is not a space-separated list — a value may contain spaces, and
// this repository writes several that do — so the conventional grammar, quoting
// included, is the only parse that agrees with what the compiler and every
// reflection-based decoder see. LookupTag is that parse; GetTagValue is the
// same lookup narrowed to the value before the first comma.
package ast
