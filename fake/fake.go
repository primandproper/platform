package fake

import (
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/go-faker/faker/v4/pkg/interfaces"
	"github.com/go-faker/faker/v4/pkg/options"
	"github.com/shoenig/test/must"
)

// Generated numbers start at 1 rather than faker's default of 0.
//
// A fake is usually handed straight to the validation of the type it fakes, and
// validation.Required rejects a zero. A generator that can emit 0 therefore turns
// every required numeric field into a test that fails a small fraction of the
// time, from a value no assertion in the test mentions — which is the least
// debuggable kind of flake there is.
//
// The upper bound is faker's own, so this is a strict subset of what it produced
// before: the same range with zero removed, not a different one.
var (
	nonZeroIntegers = interfaces.RandomIntegerBoundary{Start: 1, End: 100}
	nonZeroFloats   = interfaces.RandomFloatBoundary{Start: 1, End: 100}
)

// DefaultRecursionDepth is the recursion bound every builder in this package
// applies unless the caller names another one.
//
// faker bounds recursion per type rather than per level: it counts how many times
// each type already appears on the path down to the value it is filling, and
// writes a zero instead of recursing once that count exceeds the bound. At 0 a
// type that reaches itself — directly, or around a cycle through other types — is
// filled once, and the field that would repeat it is left zero.
//
// That is one below faker's own default of 1, and the difference is not one
// level. Every extra level is multiplied by the slices on the way down to it: a
// struct holding a slice of itself yields on the order of twenty values at 0 and
// seventy at 1, and it is the faked type's shape rather than anything the caller
// wrote that decides the multiplier. Callers who want the nested graph populated
// ask for it by depth, through the ToDepth builders.
const DefaultRecursionDepth uint = 0

// fakerOptions returns the options every builder in this package shares, bounded
// to the given recursion depth.
func fakerOptions(depth uint) []options.OptionFunc {
	return []options.OptionFunc{
		options.WithRandomIntegerBoundaries(nonZeroIntegers),
		options.WithRandomFloatBoundaries(nonZeroFloats),
		options.WithRecursionMaxDepth(depth),
	}
}

// BuildFakeTime builds a fake time, truncated to the second and in UTC.
//
// It draws from the same library every other builder here does. A second faker
// was pulled in for this one function, and two generators mean two seeds and
// two notions of what a random value is — which is a surprising amount of
// machinery for "an arbitrary timestamp".
//
// Truncated because these values round-trip through columns that do not all
// keep sub-second precision, and a fake that survives a save but not a reload
// fails an equality assertion for a reason that has nothing to do with the code
// under test.
func BuildFakeTime() time.Time {
	return time.Unix(faker.UnixTime(), 0).Truncate(time.Second).UTC()
}

// BuildFakeForTest builds a fake instance of the given type for a test, failing
// the test on error. Recursion is bounded to DefaultRecursionDepth; the caller
// who wants a deeper graph reaches for BuildFakeForTestToDepth.
func BuildFakeForTest[X any](t *testing.T) *X {
	t.Helper()

	return BuildFakeForTestToDepth[X](t, DefaultRecursionDepth)
}

// BuildFakeForTestToDepth builds a fake instance of the given type for a test
// with recursion bounded to depth, failing the test on error.
func BuildFakeForTestToDepth[X any](t *testing.T, depth uint) (x *X) {
	t.Helper()
	must.NoError(t, faker.FakeData(&x, fakerOptions(depth)...))

	return x
}

// MustBuildFake builds a fake instance of the given type, panicking on error.
// Recursion is bounded to DefaultRecursionDepth; the caller who wants a deeper
// graph reaches for MustBuildFakeToDepth.
func MustBuildFake[X any]() X {
	return MustBuildFakeToDepth[X](DefaultRecursionDepth)
}

// MustBuildFakeToDepth builds a fake instance of the given type with recursion
// bounded to depth, panicking on error.
func MustBuildFakeToDepth[X any](depth uint) X {
	x, err := BuildFakeToDepth[X](depth)
	if err != nil {
		panic(err)
	}

	return *x
}

// BuildFake builds a fake instance of the given type. Recursion is bounded to
// DefaultRecursionDepth; the caller who wants a deeper graph reaches for
// BuildFakeToDepth.
func BuildFake[X any]() (*X, error) {
	return BuildFakeToDepth[X](DefaultRecursionDepth)
}

// BuildFakeToDepth builds a fake instance of the given type with recursion
// bounded to depth.
func BuildFakeToDepth[X any](depth uint) (x *X, err error) {
	if err = faker.FakeData(&x, fakerOptions(depth)...); err != nil {
		return nil, err
	}

	return x, nil
}
