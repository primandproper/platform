package fake

import (
	"testing"
	"time"

	fake "github.com/brianvoe/gofakeit/v7"
	"github.com/go-faker/faker/v4"
	"github.com/go-faker/faker/v4/pkg/options"
	"github.com/shoenig/test/must"
)

// BuildFakeTime builds a fake time.
func BuildFakeTime() time.Time {
	return fake.Date().Add(0).Truncate(time.Second).UTC()
}

// BuildFakeForTest builds a fake instance of insert-struct-here for a test.
func BuildFakeForTest[X any](t *testing.T) (x *X) {
	t.Helper()
	must.NoError(t, faker.FakeData(&x, options.WithRecursionMaxDepth(0)))
	return x
}

// MustBuildFake builds a fake instance of insert-struct-here for a test.
func MustBuildFake[X any]() X {
	x, err := BuildFake[X]()
	if err != nil {
		panic(err)
	}

	return *x
}

// BuildFake builds a fake instance of insert-struct-here for a test.
func BuildFake[X any]() (x *X, err error) {
	if err = faker.FakeData(&x); err != nil {
		return nil, err
	}

	return x, nil
}
