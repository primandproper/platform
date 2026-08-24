package grpc

import (
	"math"
	"reflect"
	"testing"
	"time"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/filtering/filteringpb"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The instants are UTC and whole nanoseconds because that is what a Timestamp
// can carry; anything else would fail a round trip on the representation rather
// than on the conversion.
var (
	createdAfter  = time.Date(2026, time.August, 1, 12, 0, 0, 1, time.UTC)
	createdBefore = time.Date(2026, time.August, 2, 12, 0, 0, 2, time.UTC)
	updatedAfter  = time.Date(2026, time.August, 3, 12, 0, 0, 3, time.UTC)
	updatedBefore = time.Date(2026, time.August, 4, 12, 0, 0, 4, time.UTC)
)

// assertEveryFieldSet is what makes the round-trip tests hold for a field
// nobody remembered to add to them. A fixture that leaves one at its zero value
// round-trips a zero, which is what a converter that dropped the field does
// too.
func assertEveryFieldSet(t *testing.T, v any) {
	t.Helper()

	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		must.False(t, rv.IsNil())
		rv = rv.Elem()
	}

	rt := rv.Type()

	for i := range rt.NumField() {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}

		if rv.Field(i).IsZero() {
			t.Errorf("%s.%s is unset, so this fixture does not test it", rt.Name(), field.Name)
		}
	}
}

func fullQueryFilter() *filtering.QueryFilter {
	return &filtering.QueryFilter{
		SortBy:          filtering.SortDescending,
		CreatedAfter:    &createdAfter,
		CreatedBefore:   &createdBefore,
		UpdatedAfter:    &updatedAfter,
		UpdatedBefore:   &updatedBefore,
		MaxResponseSize: new(uint16(17)),
		IncludeArchived: new(true),
		Cursor:          new("cursor"),
	}
}

func TestFromProto(T *testing.T) {
	T.Parallel()

	T.Run("absent message is the default filter", func(t *testing.T) {
		t.Parallel()

		qf, err := FromProto(nil)
		must.NoError(t, err)
		test.Eq(t, filtering.DefaultQueryFilter(), qf)
	})

	T.Run("empty message is the default filter", func(t *testing.T) {
		t.Parallel()

		qf, err := FromProto(&filteringpb.QueryFilter{})
		must.NoError(t, err)
		test.Eq(t, filtering.DefaultQueryFilter(), qf)
	})

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := fullQueryFilter()
		assertEveryFieldSet(t, expected)

		qf, err := FromProto(ToProto(expected))
		must.NoError(t, err)
		test.Eq(t, expected, qf)
	})

	T.Run("page size above the ceiling clamps", func(t *testing.T) {
		t.Parallel()

		qf, err := FromProto(&filteringpb.QueryFilter{
			MaxResponseSize: new(uint32(filtering.MaxQueryFilterLimit) + 1),
		})
		must.NoError(t, err)
		must.NotNil(t, qf.MaxResponseSize)
		test.EqOp(t, filtering.MaxQueryFilterLimit, *qf.MaxResponseSize)
	})

	// The reason this package exists. Protobuf has no uint16, so a page size
	// crosses as a uint32; narrowing it before clamping turns 70000 into 4464,
	// which then clamps to a page nobody asked for and which nothing reports.
	T.Run("page size that would wrap if narrowed first", func(t *testing.T) {
		t.Parallel()

		for _, size := range []uint32{
			70000,
			1 << 16,
			(1 << 16) + uint32(filtering.MaxQueryFilterLimit),
			math.MaxUint32,
		} {
			qf, err := FromProto(&filteringpb.QueryFilter{MaxResponseSize: &size})
			must.NoError(t, err)
			must.NotNil(t, qf.MaxResponseSize)
			test.EqOp(t, filtering.MaxQueryFilterLimit, *qf.MaxResponseSize,
				test.Sprintf("page size %d", size))
		}
	})

	T.Run("page size of zero is the default", func(t *testing.T) {
		t.Parallel()

		qf, err := FromProto(&filteringpb.QueryFilter{MaxResponseSize: new(uint32(0))})
		must.NoError(t, err)
		must.NotNil(t, qf.MaxResponseSize)
		test.EqOp(t, uint16(filtering.DefaultQueryFilterLimit), *qf.MaxResponseSize)
	})

	T.Run("unrecognized sort direction is reported", func(t *testing.T) {
		t.Parallel()

		qf, err := FromProto(&filteringpb.QueryFilter{SortBy: new("sideways")})
		test.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)

		// Still usable, and still ascending, so a caller that logs and lists
		// anyway gets the page it would have got before.
		must.NotNil(t, qf.SortBy)
		test.EqOp(t, *filtering.SortAscending, *qf.SortBy)
	})

	T.Run("an empty sort direction is unrecognized too", func(t *testing.T) {
		t.Parallel()

		_, err := FromProto(&filteringpb.QueryFilter{SortBy: new("")})
		test.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
	})

	T.Run("a timestamp protobuf rejects is reported and left absent", func(t *testing.T) {
		t.Parallel()

		invalid := &timestamppb.Timestamp{Seconds: math.MaxInt64}

		qf, err := FromProto(&filteringpb.QueryFilter{
			CreatedAfter:  invalid,
			CreatedBefore: invalid,
			UpdatedAfter:  invalid,
			UpdatedBefore: invalid,
			Cursor:        new("kept"),
		})
		test.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)

		// Absent rather than the nonsense instant AsTime would have handed
		// back: a window nobody asked for excludes rows nobody would look for.
		test.Nil(t, qf.CreatedAfter)
		test.Nil(t, qf.CreatedBefore)
		test.Nil(t, qf.UpdatedAfter)
		test.Nil(t, qf.UpdatedBefore)

		// Every field is still attempted, so one bad value does not hide the
		// rest of the filter.
		must.NotNil(t, qf.Cursor)
		test.EqOp(t, "kept", *qf.Cursor)
	})

	T.Run("every unreadable field is reported, not just the first", func(t *testing.T) {
		t.Parallel()

		_, err := FromProto(&filteringpb.QueryFilter{
			CreatedAfter: &timestamppb.Timestamp{Seconds: math.MaxInt64},
			SortBy:       new("sideways"),
		})
		must.Error(t, err)

		test.StrContains(t, err.Error(), "created_after")
		test.StrContains(t, err.Error(), "sortBy")
	})
}

func TestToProto(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		in := fullQueryFilter()
		assertEveryFieldSet(t, in)

		out := ToProto(in)
		must.NotNil(t, out)

		test.EqOp(t, *in.SortBy, out.GetSortBy())
		test.EqOp(t, *in.Cursor, out.GetCursor())
		test.EqOp(t, *in.IncludeArchived, out.GetIncludeArchived())
		test.EqOp(t, uint32(*in.MaxResponseSize), out.GetMaxResponseSize())
		test.EqOp(t, createdAfter, out.GetCreatedAfter().AsTime())
		test.EqOp(t, createdBefore, out.GetCreatedBefore().AsTime())
		test.EqOp(t, updatedAfter, out.GetUpdatedAfter().AsTime())
		test.EqOp(t, updatedBefore, out.GetUpdatedBefore().AsTime())
	})

	// A url.Values cannot say "no filter" and writes the defaults out instead.
	// A message can simply not be there, and FromProto reads an absent one as
	// the default filter, so nil survives the round trip as the same request.
	T.Run("a nil filter is an absent message", func(t *testing.T) {
		t.Parallel()

		test.Nil(t, ToProto(nil))

		qf, err := FromProto(ToProto(nil))
		must.NoError(t, err)
		test.Eq(t, filtering.DefaultQueryFilter(), qf)
	})

	T.Run("absent fields stay absent", func(t *testing.T) {
		t.Parallel()

		out := ToProto(&filtering.QueryFilter{})
		must.NotNil(t, out)

		test.Nil(t, out.SortBy)
		test.Nil(t, out.Cursor)
		test.Nil(t, out.IncludeArchived)
		test.Nil(t, out.MaxResponseSize)
		test.Nil(t, out.CreatedAfter)
		test.Nil(t, out.CreatedBefore)
		test.Nil(t, out.UpdatedAfter)
		test.Nil(t, out.UpdatedBefore)
	})
}

func fullPagination() filtering.Pagination {
	return filtering.Pagination{
		AppliedQueryFilter: fullQueryFilter(),
		Cursor:             "next",
		PreviousCursor:     "previous",
		FilteredCount:      11,
		TotalCount:         22,
		MaxResponseSize:    17,
		CountsKnown:        true,
	}
}

func TestPagination(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := fullPagination()
		assertEveryFieldSet(t, expected)
		assertEveryFieldSet(t, expected.AppliedQueryFilter)

		actual, err := PaginationFromProto(PaginationToProto(expected))
		must.NoError(t, err)
		test.Eq(t, expected, actual)
	})

	T.Run("absent message is the zero Pagination", func(t *testing.T) {
		t.Parallel()

		actual, err := PaginationFromProto(nil)
		must.NoError(t, err)
		test.Eq(t, filtering.Pagination{}, actual)

		// Counts unanswered rather than answered as zero, which is the whole
		// point of the flag.
		test.False(t, actual.CountsKnown)
	})

	T.Run("counts of zero are still reported as known", func(t *testing.T) {
		t.Parallel()

		actual, err := PaginationFromProto(&filteringpb.Pagination{CountsKnown: true})
		must.NoError(t, err)

		test.True(t, actual.CountsKnown)
		test.EqOp(t, uint64(0), actual.FilteredCount)
		test.EqOp(t, uint64(0), actual.TotalCount)
	})

	T.Run("a page size that would wrap is clamped", func(t *testing.T) {
		t.Parallel()

		actual, err := PaginationFromProto(&filteringpb.Pagination{MaxResponseSize: 70000})
		must.NoError(t, err)
		test.EqOp(t, filtering.MaxQueryFilterLimit, actual.MaxResponseSize)
	})

	// The applied filter reports what a page was answered under. Defaulting it
	// here would describe a page that was never served.
	T.Run("the applied filter is not normalized", func(t *testing.T) {
		t.Parallel()

		actual, err := PaginationFromProto(&filteringpb.Pagination{
			AppliedQueryFilter: &filteringpb.QueryFilter{Cursor: new("cursor")},
		})
		must.NoError(t, err)
		must.NotNil(t, actual.AppliedQueryFilter)

		test.Nil(t, actual.AppliedQueryFilter.MaxResponseSize)
		test.Nil(t, actual.AppliedQueryFilter.SortBy)
	})

	T.Run("an unreadable applied filter is reported", func(t *testing.T) {
		t.Parallel()

		actual, err := PaginationFromProto(&filteringpb.Pagination{
			AppliedQueryFilter: &filteringpb.QueryFilter{
				CreatedAfter: &timestamppb.Timestamp{Seconds: math.MaxInt64},
			},
		})
		test.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
		test.StrContains(t, err.Error(), "applied_query_filter")

		must.NotNil(t, actual.AppliedQueryFilter)
		test.Nil(t, actual.AppliedQueryFilter.CreatedAfter)
	})

	T.Run("an absent applied filter stays absent", func(t *testing.T) {
		t.Parallel()

		out := PaginationToProto(filtering.Pagination{})
		must.NotNil(t, out)
		test.Nil(t, out.AppliedQueryFilter)

		actual, err := PaginationFromProto(out)
		must.NoError(t, err)
		test.Nil(t, actual.AppliedQueryFilter)
	})

	T.Run("a filter's pagination crosses unchanged", func(t *testing.T) {
		t.Parallel()

		qf := fullQueryFilter()

		expected := qf.ToPagination()
		actual, err := PaginationFromProto(PaginationToProto(expected))
		must.NoError(t, err)

		test.Eq(t, expected, actual)
	})
}
