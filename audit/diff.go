package audit

import (
	"reflect"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v8/errors"
)

var (
	// ErrNotAStruct indicates a Diff argument that is neither a struct nor a
	// pointer to one.
	ErrNotAStruct = platformerrors.New("audit diff requires a struct or a pointer to one")

	// ErrDiffTypeMismatch indicates a Diff of two different struct types. There
	// is no sensible field-by-field answer for such a pair, and producing one
	// anyway would record a change that never happened.
	ErrDiffTypeMismatch = platformerrors.New("audit diff requires both values to be the same type")

	// ErrNothingToDiff indicates a Diff where both sides were nil.
	ErrNothingToDiff = platformerrors.New("audit diff requires at least one non-nil value")
)

// Diff reports the fields that differ between two versions of a resource, in
// the shape Entry.Changes wants. Identical values produce an empty map, not
// nil, so the result can be assigned to an Entry unconditionally.
//
// It exists because the alternative is every mutation site in a codebase
// hand-assembling a change map, which is tedious where it is right and silently
// incomplete where it is wrong — the field somebody forgot to add to the map
// when they added it to the struct is exactly the field an investigation will
// want.
//
//	entry.Changes, err = audit.Diff(before, after)
//
// Either side may be nil, for a creation or a deletion. A nil side is treated
// as the zero value of the other's type and then omitted from the result, so
// Diff(nil, x) reports x's non-zero fields as additions with no Old, and
// Diff(x, nil) reports them as removals with no New.
//
// Field names come from the json tag where there is one and the field name
// otherwise, so the audit log and the API speak the same vocabulary. A field
// tagged json:"-" is skipped, as is one tagged audit:"-" — which is the
// compile-time counterpart to Redaction: use the tag for a field that must
// never be audited wherever it appears, and a Redaction for a policy that
// belongs to a deployment rather than to the type.
//
// Comparison is by reflect.DeepEqual at the top level of the struct. Embedded
// structs are flattened, matching how encoding/json promotes them, but a named
// struct field is compared and recorded whole rather than descended into: a
// change to one member of a nested struct records the whole nested value on
// both sides. That is the right altitude for an audit log — the question it
// answers is which field of the resource changed — and a caller who needs finer
// granularity can Diff the nested values separately.
func Diff(before, after any) (map[string]Change, error) {
	beforeValue, haveBefore, err := structValue(before)
	if err != nil {
		return nil, err
	}

	afterValue, haveAfter, err := structValue(after)
	if err != nil {
		return nil, err
	}

	switch {
	case !haveBefore && !haveAfter:
		return nil, ErrNothingToDiff
	case !haveBefore:
		beforeValue = reflect.Zero(afterValue.Type())
	case !haveAfter:
		afterValue = reflect.Zero(beforeValue.Type())
	case beforeValue.Type() != afterValue.Type():
		return nil, platformerrors.Wrapf(
			ErrDiffTypeMismatch, "%s against %s", beforeValue.Type(), afterValue.Type(),
		)
	}

	changes := map[string]Change{}
	collectChanges(changes, beforeValue, afterValue, haveBefore, haveAfter)

	// An empty map rather than nil for "nothing changed", so a caller can assign
	// the result straight onto an Entry without checking. Record collapses the
	// two to the same stored value anyway, so an entry recording no changes
	// hashes identically however its call site spelled it.
	return changes, nil
}

// structValue resolves an argument to an addressable struct value, reporting
// whether there was one at all. A nil argument, a nil pointer, and an untyped
// nil interface all mean "this side does not exist".
func structValue(v any) (value reflect.Value, present bool, err error) {
	if v == nil {
		return reflect.Value{}, false, nil
	}

	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return reflect.Value{}, false, nil
	}

	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return reflect.Value{}, false, nil
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return reflect.Value{}, false, platformerrors.Wrapf(ErrNotAStruct, "got %s", rv.Kind())
	}

	return rv, true, nil
}

// collectChanges walks one struct level, recursing through embedded structs.
func collectChanges(changes map[string]Change, beforeValue, afterValue reflect.Value, haveBefore, haveAfter bool) {
	t := beforeValue.Type()

	for i := range t.NumField() {
		field := t.Field(i)

		if field.Tag.Get("audit") == "-" {
			continue
		}

		name, explicit, skip := fieldName(&field)
		if skip {
			continue
		}

		beforeField, afterField := beforeValue.Field(i), afterValue.Field(i)

		// An embedded struct with no json name of its own is flattened, exactly
		// as encoding/json would promote its fields. One carrying an explicit
		// name is a named object in the encoded form and is recorded as one.
		//
		// This runs before the exported check, and has to: an embedded field's
		// name is its type's name, so `struct { base }` is an unexported field
		// whose exported members are nonetheless promoted — by encoding/json,
		// and so by this. reflect agrees, allowing those members to be read even
		// though the embedded field itself cannot be.
		if field.Anonymous && !explicit {
			if embeddedBefore, embeddedAfter, ok := embeddedStructs(beforeField, afterField); ok {
				collectChanges(changes, embeddedBefore, embeddedAfter, haveBefore, haveAfter)

				continue
			}
		}

		if !field.IsExported() || !beforeField.CanInterface() || !afterField.CanInterface() {
			continue
		}

		oldValue, newValue := beforeField.Interface(), afterField.Interface()
		if reflect.DeepEqual(oldValue, newValue) {
			continue
		}

		change := Change{}
		if haveBefore {
			change.Old = oldValue
		}
		if haveAfter {
			change.New = newValue
		}

		changes[name] = change
	}
}

// embeddedStructs resolves an embedded field on both sides to struct values,
// substituting a zero struct for a nil embedded pointer so that a resource that
// gained or lost one still diffs field by field rather than not at all.
func embeddedStructs(beforeField, afterField reflect.Value) (beforeOut, afterOut reflect.Value, ok bool) {
	if beforeField.Kind() == reflect.Pointer {
		if beforeField.Type().Elem().Kind() != reflect.Struct {
			return reflect.Value{}, reflect.Value{}, false
		}

		return derefOrZero(beforeField), derefOrZero(afterField), true
	}

	if beforeField.Kind() != reflect.Struct {
		return reflect.Value{}, reflect.Value{}, false
	}

	return beforeField, afterField, true
}

// derefOrZero dereferences a pointer, yielding the zero value of its element
// type when it is nil.
func derefOrZero(v reflect.Value) reflect.Value {
	if v.IsNil() {
		return reflect.Zero(v.Type().Elem())
	}

	return v.Elem()
}

// fieldName resolves the name a field is recorded under, reporting whether the
// name was given explicitly by a json tag and whether the field is skipped
// entirely.
func fieldName(field *reflect.StructField) (name string, explicit, skip bool) {
	tag, ok := field.Tag.Lookup("json")
	if !ok {
		return field.Name, false, false
	}

	tagged, _, _ := strings.Cut(tag, ",")

	switch tagged {
	case "-":
		// json:"-" omits the field; json:"-," names it "-". The distinction is
		// encoding/json's, and it is honored here so that the audit log and the
		// encoded resource never disagree about which fields exist.
		if strings.HasPrefix(tag, "-,") {
			return "-", true, false
		}

		return "", false, true
	case "":
		return field.Name, false, false
	default:
		return tagged, true, false
	}
}
