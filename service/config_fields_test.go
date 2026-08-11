package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// TestEverySubConfigIsValidated asserts that every pointer sub-config field of
// Config appears in ValidateWithContext's field list.
//
// It exists because the one thing that goes wrong here goes wrong silently. A
// subsystem added to the struct and to Register but not to the validation list
// is registered, built, and run from a config nothing ever checked — and there
// is no failing test to write for it after the fact, because what it does is
// accept configurations it should have refused. Shredding sat like that: the
// only one of thirty-seven sub-configs missing from the list.
//
// It reads the source rather than the values because the omission is a
// statically visible fact about the code, and every value-level approach needs
// an invalid instance of each sub-config type to detect anything.
func TestEverySubConfigIsValidated(t *testing.T) {
	t.Parallel()

	validated := validatedFieldNames(t)

	cfgType := reflect.TypeFor[Config]()
	for field := range cfgType.Fields() {
		if !field.IsExported() || field.Type.Kind() != reflect.Pointer || field.Type.Elem().Kind() != reflect.Struct {
			continue
		}

		test.True(t, validated[field.Name],
			test.Sprintf("Config.%s is a sub-config that ValidateWithContext never names, so nothing checks it", field.Name))
	}
}

// validatedFieldNames returns the Config field names ValidateWithContext hands
// to ozzo, read from the `validation.Field(&cfg.X, ...)` calls in its body.
func validatedFieldNames(t *testing.T) map[string]bool {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "config.go", nil, 0)
	must.NoError(t, err)

	names := map[string]bool{}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "ValidateWithContext" || fn.Recv == nil {
			continue
		}

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall || len(call.Args) == 0 || !isValidationField(call.Fun) {
				return true
			}

			unary, isUnary := call.Args[0].(*ast.UnaryExpr)
			if !isUnary || unary.Op != token.AND {
				return true
			}

			if sel, isSel := unary.X.(*ast.SelectorExpr); isSel {
				names[sel.Sel.Name] = true
			}

			return true
		})
	}

	must.MapNotEmpty(t, names, must.Sprint("found no validation.Field calls; the parse, not the config, is what broke"))

	return names
}

// isValidationField reports whether fun names ozzo's validation.Field.
func isValidationField(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Field" {
		return false
	}

	pkg, ok := sel.X.(*ast.Ident)

	return ok && pkg.Name == "validation"
}
