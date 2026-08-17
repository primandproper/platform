package fake

import (
	"math"
	"testing"
	"time"

	"github.com/go-faker/faker/v4/pkg/interfaces"
	"github.com/go-faker/faker/v4/pkg/options"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// record is a stand-in for a stored type, with one field of each kind
// BuildFakeRecord makes a decision about.
type record struct {
	CreatedAt        time.Time
	ArchivedAt       *time.Time
	Changes          map[string]any
	Children         []*recordChild
	ID               string
	BelongsToAccount string
	CreatedByUser    string
	Description      string
	Quantity         float32
	Count            uint16
	Nested           recordChild
}

type recordChild struct {
	ID   string
	Name string
}

func TestBuildFakeRecord(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		actual := BuildFakeRecord[record]()

		must.NotNil(t, actual)
		test.NotEq(t, "", actual.Description)
		test.NotEq(t, uint16(0), actual.Count)
	})

	T.Run("gives identifier-shaped fields identifiers", func(t *testing.T) {
		t.Parallel()

		actual := BuildFakeRecord[record]()

		// The identifiers this platform issues are a fixed width, which the random
		// strings faker writes into every other string field are not.
		test.EqOp(t, len(BuildFakeID()), len(actual.ID))
		test.EqOp(t, len(BuildFakeID()), len(actual.BelongsToAccount))
		test.EqOp(t, len(BuildFakeID()), len(actual.CreatedByUser))
		test.EqOp(t, len(BuildFakeID()), len(actual.Nested.ID))
		test.NotEqOp(t, actual.ID, actual.BelongsToAccount)
	})

	T.Run("leaves optional fields and child collections absent", func(t *testing.T) {
		t.Parallel()

		actual := BuildFakeRecord[record]()

		test.Nil(t, actual.ArchivedAt)
		test.Nil(t, actual.Children)
		test.Nil(t, actual.Changes)
	})

	T.Run("times are truncated and in UTC", func(t *testing.T) {
		t.Parallel()

		actual := BuildFakeRecord[record]()

		test.False(t, actual.CreatedAt.IsZero())
		test.EqOp(t, time.UTC, actual.CreatedAt.Location())
		test.EqOp(t, actual.CreatedAt.Truncate(time.Second), actual.CreatedAt)
	})

	T.Run("floats are whole numbers", func(t *testing.T) {
		t.Parallel()

		for range 20 {
			actual := BuildFakeRecord[record]()

			test.NotEq(t, float32(0), actual.Quantity)
			test.EqOp(t, math.Trunc(float64(actual.Quantity)), float64(actual.Quantity))
		}
	})

	T.Run("the caller's option wins over the ones here", func(t *testing.T) {
		t.Parallel()

		actual := BuildFakeRecord[record](
			options.WithRandomIntegerBoundaries(interfaces.RandomIntegerBoundary{Start: 7, End: 8}),
		)

		test.EqOp(t, uint16(7), actual.Count)
	})
}

func Test_isIdentifier(T *testing.T) {
	T.Parallel()

	T.Run("recognizes the names the template gives identifiers", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{"ID", "RecipeID", "OptionIDs", "BelongsToAccount", "CreatedByUser"} {
			test.True(t, isIdentifier(name), test.Sprintf("%s", name))
		}
	})

	T.Run("leaves every other name alone", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{"Name", "Description", "Identifier", "Status"} {
			test.False(t, isIdentifier(name), test.Sprintf("%s", name))
		}
	})
}
