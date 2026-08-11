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

// fakerOptions returns the options every builder in this package shares, followed
// by any the caller adds.
func fakerOptions(extra ...options.OptionFunc) []options.OptionFunc {
	return append([]options.OptionFunc{
		options.WithRandomIntegerBoundaries(nonZeroIntegers),
		options.WithRandomFloatBoundaries(nonZeroFloats),
	}, extra...)
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

// BuildFakeForTest builds a fake instance of the given type for a test, failing the test on error.
func BuildFakeForTest[X any](t *testing.T) (x *X) {
	t.Helper()
	must.NoError(t, faker.FakeData(&x, fakerOptions(options.WithRecursionMaxDepth(0))...))
	return x
}

// MustBuildFake builds a fake instance of the given type, panicking on error.
func MustBuildFake[X any]() X {
	x, err := BuildFake[X]()
	if err != nil {
		panic(err)
	}

	return *x
}

// BuildFake builds a fake instance of the given type.
func BuildFake[X any]() (x *X, err error) {
	if err = faker.FakeData(&x, fakerOptions()...); err != nil {
		return nil, err
	}

	return x, nil
}
