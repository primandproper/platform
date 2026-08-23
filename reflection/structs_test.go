package reflection

import (
	"reflect"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

type taggedStruct struct {
	WithOptions  string `json:"withOptions,omitempty"`
	Renamed      string `json:"renamed"`
	EmptyTag     string `json:""`
	Omitted      string `json:"-"`
	Untagged     string
	OtherTagOnly string `yaml:"otherTagOnly"`
}

// ExportedEmbed is exported because an explicitly named embedded field is
// compared as a whole value, and reflect cannot read the value of a field whose
// name — which for an embedded field is its type's name — is unexported.
type ExportedEmbed struct {
	Field1 string `json:"field1"`
}

type namedEmbedParent struct {
	ExportedEmbed `json:"embedded"`

	Field3 string `json:"field3"`
}

type unreadableNamedEmbedParent struct {
	exampleStruct `json:"embedded"`

	Field3 string `json:"field3"`
}

func TestStructValue(T *testing.T) {
	T.Parallel()

	T.Run("resolves a struct", func(t *testing.T) {
		t.Parallel()

		value, present, err := StructValue(exampleStruct{Field1: "a"})
		must.NoError(t, err)
		must.True(t, present)
		test.EqOp(t, "a", value.Field(0).String())
	})

	T.Run("resolves a pointer to a struct", func(t *testing.T) {
		t.Parallel()

		value, present, err := StructValue(&exampleStruct{Field1: "a"})
		must.NoError(t, err)
		must.True(t, present)
		test.EqOp(t, "a", value.Field(0).String())
	})

	T.Run("reports the three flavors of absent without erroring", func(t *testing.T) {
		t.Parallel()

		var nilPointer *exampleStruct

		// An untyped nil and a typed nil pointer are both "there is nothing
		// here", and neither is a failure — whether they are fatal belongs to
		// the caller.
		for name, arg := range map[string]any{
			"untyped nil": nil,
			"nil pointer": nilPointer,
		} {
			value, present, err := StructValue(arg)
			must.NoError(t, err, must.Sprintf("for %s", name))
			test.False(t, present, test.Sprintf("for %s", name))
			test.False(t, value.IsValid(), test.Sprintf("for %s", name))
		}
	})

	T.Run("rejects a non-struct", func(t *testing.T) {
		t.Parallel()

		_, present, err := StructValue("not a struct")
		test.ErrorIs(t, err, ErrNotAStruct)
		test.False(t, present)
	})

	T.Run("rejects a pointer to a non-struct", func(t *testing.T) {
		t.Parallel()

		s := "not a struct"

		_, present, err := StructValue(&s)
		test.ErrorIs(t, err, ErrNotAStruct)
		test.False(t, present)
	})
}

func TestStructType(T *testing.T) {
	T.Parallel()

	T.Run("accepts a nil pointer, unlike StructValue", func(t *testing.T) {
		t.Parallel()

		var nilPointer *exampleStruct

		// This is the whole reason the two exist separately: a nil *T has no
		// fields to read but does have fields to describe.
		typ, err := StructType(nilPointer)
		must.NoError(t, err)
		test.EqOp(t, 2, typ.NumField())

		_, present, valueErr := StructValue(nilPointer)
		must.NoError(t, valueErr)
		test.False(t, present)
	})

	T.Run("resolves values, pointers, and reflect.Type alike", func(t *testing.T) {
		t.Parallel()

		pointer := &exampleStruct{}
		doublePointer := &pointer

		for name, arg := range map[string]any{
			"value":          exampleStruct{},
			"pointer":        pointer,
			"double pointer": doublePointer,
			"reflect.Type":   reflect.TypeFor[exampleStruct](),
			"pointer type":   reflect.TypeFor[*exampleStruct](),
		} {
			typ, err := StructType(arg)
			must.NoError(t, err, must.Sprintf("for %s", name))
			test.EqOp(t, "exampleStruct", typ.Name(), test.Sprintf("for %s", name))
		}
	})

	T.Run("rejects nil and non-structs", func(t *testing.T) {
		t.Parallel()

		_, err := StructType(nil)
		test.ErrorIs(t, err, ErrNoValue)

		_, err = StructType("not a struct")
		test.ErrorIs(t, err, ErrNotAStruct)

		_, err = StructType(reflect.TypeFor[string]())
		test.ErrorIs(t, err, ErrNotAStruct)
	})
}

func TestFieldName(T *testing.T) {
	T.Parallel()

	// The fields are synthesized rather than declared on a real struct because
	// one of the cases — the tag `json:"-,"`, which names a field "-" — is
	// exactly the thing staticcheck's SA5008 exists to flag as a typo. It is a
	// typo in nearly every struct that has it and deliberate here, and a
	// StructTag built by hand tests the parsing without planting a lint
	// suppression in a struct that would then have to explain itself.
	cases := map[string]struct {
		fieldName string
		tag       string
		expected  string
		explicit  bool
		skip      bool
	}{
		// The defect this package shipped with: the options came back glued to
		// the name, so a field tagged json:"withOptions,omitempty" reported
		// "withOptions,omitempty" — a string no encoder would ever write.
		"strips tag options":            {fieldName: "WithOptions", tag: `json:"withOptions,omitempty"`, expected: "withOptions", explicit: true},
		"uses an explicit name":         {fieldName: "Renamed", tag: `json:"renamed"`, expected: "renamed", explicit: true},
		"falls back on an empty tag":    {fieldName: "EmptyTag", tag: `json:""`, expected: "EmptyTag"},
		"falls back with no tag at all": {fieldName: "Untagged", expected: "Untagged"},
		"falls back on another key":     {fieldName: "OtherTagOnly", tag: `yaml:"otherTagOnly"`, expected: "OtherTagOnly"},
		"skips an omitted field":        {fieldName: "Omitted", tag: `json:"-"`, skip: true},
		// json:"-" omits; json:"-," names the field "-". encoding/json's
		// distinction, honored so a description and an encoding agree.
		"honors the literal dash name": {fieldName: "LiteralDash", tag: `json:"-,"`, expected: "-", explicit: true},
		// Options after the literal dash name are still options.
		"strips options after a literal dash": {fieldName: "LiteralDashOpts", tag: `json:"-,omitempty"`, expected: "-", explicit: true},
	}

	for name, tc := range cases {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			field := reflect.StructField{
				Name: tc.fieldName,
				Type: reflect.TypeFor[string](),
				Tag:  reflect.StructTag(tc.tag),
			}

			gotName, explicit, skip := FieldName(&field, "json")
			test.EqOp(t, tc.skip, skip)
			test.EqOp(t, tc.explicit, explicit)
			test.EqOp(t, tc.expected, gotName)
		})
	}
}

func TestDerefOrZero(T *testing.T) {
	T.Parallel()

	T.Run("returns a non-pointer unchanged", func(t *testing.T) {
		t.Parallel()

		value := DerefOrZero(reflect.ValueOf(exampleStruct{Field1: "a"}))
		test.EqOp(t, "a", value.Field(0).String())
	})

	T.Run("dereferences a live pointer", func(t *testing.T) {
		t.Parallel()

		value := DerefOrZero(reflect.ValueOf(&exampleStruct{Field1: "a"}))
		test.EqOp(t, "a", value.Field(0).String())
	})

	T.Run("substitutes a zero value for a nil pointer", func(t *testing.T) {
		t.Parallel()

		var nilPointer *exampleStruct

		value := DerefOrZero(reflect.ValueOf(nilPointer))
		must.EqOp(t, reflect.Struct, value.Kind())
		test.EqOp(t, "", value.Field(0).String())
	})
}

func TestGetTagNameByValue_TagResolution(T *testing.T) {
	T.Parallel()

	T.Run("strips tag options from the reported name", func(t *testing.T) {
		t.Parallel()

		x := taggedStruct{WithOptions: "unique_options_value"}

		actual, err := GetTagNameByValue(x, "unique_options_value", "json")
		must.NoError(t, err)
		test.EqOp(t, "withOptions", actual)
	})

	T.Run("falls back to the field name when untagged", func(t *testing.T) {
		t.Parallel()

		x := taggedStruct{Untagged: "unique_untagged_value"}

		// Previously this reported "" with a nil error, which reads as "found
		// it, and it is called nothing".
		actual, err := GetTagNameByValue(x, "unique_untagged_value", "json")
		must.NoError(t, err)
		test.EqOp(t, "Untagged", actual)
	})

	T.Run("never matches a field the tag omits", func(t *testing.T) {
		t.Parallel()

		x := taggedStruct{Omitted: "unique_omitted_value"}

		_, err := GetTagNameByValue(x, "unique_omitted_value", "json")
		test.ErrorIs(t, err, ErrNoMatchingField)
	})

	T.Run("reports a missing field as ErrNoMatchingField", func(t *testing.T) {
		t.Parallel()

		_, err := GetTagNameByValue(exampleStruct{}, "nothing holds this", "json")
		test.ErrorIs(t, err, ErrNoMatchingField)
	})

	T.Run("reports absent arguments as ErrNoValue", func(t *testing.T) {
		t.Parallel()

		var nilPointer *exampleStruct

		_, err := GetTagNameByValue(nil, "x", "json")
		test.ErrorIs(t, err, ErrNoValue)

		_, err = GetTagNameByValue(nilPointer, "x", "json")
		test.ErrorIs(t, err, ErrNoValue)
	})

	T.Run("reports a non-struct as ErrNotAStruct", func(t *testing.T) {
		t.Parallel()

		_, err := GetTagNameByValue("not a struct", "x", "json")
		test.ErrorIs(t, err, ErrNotAStruct)
	})

	T.Run("compares an explicitly named embedded struct whole", func(t *testing.T) {
		t.Parallel()

		inner := ExportedEmbed{Field1: "inner_value"}
		x := namedEmbedParent{ExportedEmbed: inner, Field3: "f3"}

		// Carrying a json name makes the embedded struct a named object in the
		// encoded form, so it is matched as a value rather than searched
		// through — and its members are not promoted.
		actual, err := GetTagNameByValue(x, inner, "json")
		must.NoError(t, err)
		test.EqOp(t, "embedded", actual)

		_, err = GetTagNameByValue(x, "inner_value", "json")
		test.ErrorIs(t, err, ErrNoMatchingField)
	})

	T.Run("finds nothing in an explicitly named embed of an unexported type", func(t *testing.T) {
		t.Parallel()

		x := unreadableNamedEmbedParent{
			Field1: "inner_value",
			Field3: "f3",
		}

		// The tag asks for the embed to be treated as a whole value, but an
		// embedded field's name is its type's name, so this one is unexported
		// and reflect refuses to read it. Neither promoted nor comparable, it
		// is simply invisible — a limitation of the language, not a choice.
		_, err := GetTagNameByValue(x, "inner_value", "json")
		test.ErrorIs(t, err, ErrNoMatchingField)

		actual, err := GetTagNameByValue(x, "f3", "json")
		must.NoError(t, err)
		test.EqOp(t, "field3", actual)
	})

	T.Run("promotes an unnamed embedded struct's fields", func(t *testing.T) {
		t.Parallel()

		x := embeddedParent{
			Field1: "promoted_value",
			Field3: "f3",
		}

		actual, err := GetTagNameByValue(x, "promoted_value", "json")
		must.NoError(t, err)
		test.EqOp(t, "field1", actual)
	})

	T.Run("skips a nil embedded pointer rather than substituting a zero", func(t *testing.T) {
		t.Parallel()

		x := pointerEmbeddedParent{exampleStruct: nil, Field3: "f3"}

		// The zero-substitution DerefOrZero performs would be actively wrong
		// here: every zero field of the stand-in would match this zero needle,
		// so an absent embedded struct would answer for a field that holds
		// nothing.
		_, err := GetTagNameByValue(x, "", "json")
		test.ErrorIs(t, err, ErrNoMatchingField)
	})
}
