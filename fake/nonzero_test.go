package fake

import (
	"reflect"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// numbers covers every numeric kind faker dispatches on, so a future change to
// one branch of its type switch cannot reintroduce a zero unnoticed.
type numbers struct {
	F64 float64
	U64 uint64
	I64 int64
	U   uint
	I   int
	F32 float32
	U32 uint32
	I32 int32
	U16 uint16
	I16 int16
	U8  uint8
	I8  int8
}

// iterations is high enough that a single zero-capable field would fail this
// essentially every run: faker's default range was 100 wide, so a zero-capable
// field lands on zero about once per hundred draws.
const iterations = 2_000

func TestBuildersNeverGenerateZero(T *testing.T) {
	T.Parallel()

	assertNoZeroField := func(t *testing.T, n *numbers) {
		t.Helper()

		v := reflect.ValueOf(*n)
		for i := range v.NumField() {
			if v.Field(i).IsZero() {
				t.Fatalf("%s generated a zero value", v.Type().Field(i).Name)
			}
		}
	}

	T.Run("BuildFakeForTest", func(t *testing.T) {
		t.Parallel()

		for range iterations {
			assertNoZeroField(t, BuildFakeForTest[numbers](t))
		}
	})

	T.Run("BuildFake", func(t *testing.T) {
		t.Parallel()

		for range iterations {
			n, err := BuildFake[numbers]()
			must.NoError(t, err)
			assertNoZeroField(t, n)
		}
	})

	T.Run("MustBuildFake", func(t *testing.T) {
		t.Parallel()

		for range iterations {
			n := MustBuildFake[numbers]()
			assertNoZeroField(t, &n)
		}
	})
}

func TestGeneratedNumbersStayWithinFakerDefaultRange(T *testing.T) {
	T.Parallel()

	// The point of shifting the lower bound rather than widening the range: every
	// value a caller can now see is one it could already have seen before.
	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		for range iterations {
			n := BuildFakeForTest[numbers](t)

			test.Greater(t, 0, n.I)
			test.Less(t, 100, n.I)
			test.Greater(t, 0.0, n.F64)
			test.Less(t, 100.0, n.F64)
		}
	})
}
