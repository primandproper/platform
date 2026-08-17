package fake

import (
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v11/identifiers"

	"github.com/go-faker/faker/v4/pkg/options"
)

// BuildFakeID builds a fake identifier of the kind this platform issues.
//
// A random string is not one: identifiers are what rows join on, so a fake carrying
// twenty-five random letters where an identifier goes is a row that joins to nothing,
// and the test that fails is several layers away from the fake that caused it.
func BuildFakeID() string {
	return identifiers.New()
}

// BuildFakeNumber builds a whole number from the range generated numeric fields draw
// from.
//
// Whole for the reason BuildFakeRecord rounds the floats it generates: the columns
// these values round-trip through mostly keep two decimal places, and one that keeps
// fewer than the value does returns something the caller did not save.
func BuildFakeNumber() float64 {
	return float64(MustBuildFake[int]())
}

// BuildFakeRecord builds a fake X shaped like something this platform stores.
//
// The builders above hand back what faker produced, which for a type that is written
// to a database and read back is not quite a usable value: an identifier field gets
// random letters, a timestamp gets sub-second precision in local time, and every
// pointer and slice gets filled, which is the opposite of what an optional field and a
// child collection mean to the store. A caller then either fixes all of that by hand,
// which is the composite literal they were trying to stop writing, or writes tests
// that fail for reasons that have nothing to do with the code under test.
//
// So the value is generated and then walked, and what comes back is:
//
//   - a field named like an identifier holds a real identifier
//   - a time.Time is truncated to the second and in UTC
//   - a pointer, slice, map or interface is nil
//   - a float is a whole number
//   - everything else is what faker produced
//
// What that leaves for the caller is the fields whose values the type constrains — a
// status from a closed set, a URL that has to resolve, a minimum that has to be below
// its maximum — which are the fields worth reading in a builder anyway.
//
// The caller's options are applied over the two this needs, so an option named here
// can still be overridden; see BuildFakeForType for why order settles that. It panics
// for the reason MustBuildFake does.
func BuildFakeRecord[X any](opts ...options.OptionFunc) *X {
	x := BuildFakeForType[X](append([]options.OptionFunc{
		// A field typed any is one faker refuses to fill, and it reports that as an
		// error for the whole value rather than for the field — so a single map[string]any
		// on a type means no fake at all. Skipping it is the answer normalize gives every
		// other map anyway: absent, for the caller to fill if it means something.
		options.WithIgnoreInterface(true),

		// Faker picks each slice length at random up to a hundred, at every level, so a
		// type three collections deep is a graph of millions that costs seconds to build.
		// Every one of those values is discarded below. One is the smallest length the
		// option accepts.
		options.WithRandomMapAndSliceMaxSize(1),
	}, opts...)...)

	normalize(reflect.ValueOf(x).Elem())

	return x
}

var timeType = reflect.TypeFor[time.Time]()

// normalize applies this package's conventions to an already-generated value in place.
func normalize(v reflect.Value) {
	if v.Type() == timeType {
		v.Set(reflect.ValueOf(BuildFakeTime()))

		return
	}

	if v.Kind() != reflect.Struct {
		return
	}

	for i := range v.NumField() {
		field, value := v.Type().Field(i), v.Field(i)
		if !field.IsExported() {
			continue
		}

		switch value.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface, reflect.Chan, reflect.Func:
			value.Set(reflect.Zero(value.Type()))
		case reflect.String:
			if isIdentifier(field.Name) {
				value.SetString(BuildFakeID())
			}
		case reflect.Float32, reflect.Float64:
			// The generated range starts at one, so rounding cannot produce a zero and
			// turn a required field into a failure of a different kind.
			value.SetFloat(math.Round(value.Float()))
		case reflect.Struct:
			normalize(value)
		default:
		}
	}
}

// isIdentifier reports whether a field holds an identifier, judged by its name.
//
// The names are the convention the template keeps: an ID, a foreign key spelled
// BelongsToX, or the user who did something. Judging by name rather than by type is a
// heuristic, and the cost of it being wrong is asymmetric — a field wrongly treated as
// an identifier holds a well-formed value of the right shape that nothing joins on,
// which is what it would have held anyway. A consumer whose names differ says so in
// the builder, the same way it does for every other constrained field.
func isIdentifier(name string) bool {
	switch {
	case name == "ID", strings.HasSuffix(name, "ID"), strings.HasSuffix(name, "IDs"):
		return true
	case strings.HasPrefix(name, "BelongsTo"):
		return true
	case strings.HasSuffix(name, "User"):
		return true
	default:
		return false
	}
}
