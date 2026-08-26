package grpc

import (
	"time"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/filtering/filteringpb"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// The proto field names, for error messages that name the field a caller
// actually sent rather than the query parameter with the same meaning. They are
// literals because a descriptor lookup to build an error string is a lot of
// machinery for four words; TestFieldNames is what keeps them from naming a
// field the schema does not have.
const (
	fieldCreatedAfter  = "created_after"
	fieldCreatedBefore = "created_before"
	fieldUpdatedAfter  = "updated_after"
	fieldUpdatedBefore = "updated_before"
	fieldAppliedFilter = "applied_query_filter"
)

// FromProto converts a wire QueryFilter into the Go one, applying the ceiling
// before the narrowing and then the defaults.
//
// An absent message is the default filter rather than an empty one: a client
// that sent no filter asked for the first page at the default size, which is
// what every other transport here reads it as.
//
// The page size is the field this function exists for. Protobuf has no uint16,
// so max_response_size crosses as a uint32 and something has to narrow it;
// doing that before the clamp is silent, because by the time a QueryFilter
// exists the wrapped value is indistinguishable from one a client sent.
// SetMaxResponseSize takes the wide value and applies the ceiling in the order
// that works. Normalize then supplies the default for a page size that was
// absent or zero, which is what the HTTP path does with the same two cases.
//
// The returned filter is always usable. A timestamp protobuf itself considers
// out of range is left absent and reported, and a sort direction nobody
// recognizes is reported by Normalize with ascending left in place — both
// wrapping errors.ErrUnrecognizedInputValue. A caller reporting the failure
// should discard the filter rather than list against a half-applied one.
func FromProto(in *filteringpb.QueryFilter) (*filtering.QueryFilter, error) {
	if in == nil {
		return filtering.DefaultQueryFilter(), nil
	}

	qf, err := fromProto(in)

	// Normalize supplies the default page size for an absent one, clamps a
	// present one again — harmlessly, since SetMaxResponseSize already did —
	// and reports an unrecognized sort direction. Joined rather than returned
	// on its own so a field that would not decode is not lost behind a sort
	// direction that happened to be fine.
	return qf, platformerrors.Join(err, qf.Normalize())
}

// fromProto copies the fields across without normalizing, which is what the
// response half wants: a Pagination reports the filter that was applied, and
// applying defaults to it again would describe a page that was never served.
func fromProto(in *filteringpb.QueryFilter) (*filtering.QueryFilter, error) {
	var errs []error

	qf := &filtering.QueryFilter{}

	// readTime records an error only for a timestamp that was sent and that
	// protobuf itself rejects — one outside the year range the type allows.
	// AsTime would hand back a nonsense instant for that rather than fail, and a
	// window nobody asked for excludes rows nobody would think to look for.
	readTime := func(field string, ts *timestamppb.Timestamp, into **time.Time) {
		if ts == nil {
			return
		}

		if err := ts.CheckValid(); err != nil {
			errs = append(errs, platformerrors.Wrapf(
				platformerrors.Join(platformerrors.ErrUnrecognizedInputValue, err),
				"reading %s field %q", field, ts.String(),
			))

			return
		}

		*into = new(ts.AsTime())
	}

	readTime(fieldCreatedAfter, in.GetCreatedAfter(), &qf.CreatedAfter)
	readTime(fieldCreatedBefore, in.GetCreatedBefore(), &qf.CreatedBefore)
	readTime(fieldUpdatedAfter, in.GetUpdatedAfter(), &qf.UpdatedAfter)
	readTime(fieldUpdatedBefore, in.GetUpdatedBefore(), &qf.UpdatedBefore)

	if in.SortBy != nil {
		qf.SortBy = new(in.GetSortBy())
	}

	if in.Cursor != nil {
		qf.Cursor = new(in.GetCursor())
	}

	if in.IncludeArchived != nil {
		qf.IncludeArchived = new(in.GetIncludeArchived())
	}

	// Absent stays absent rather than arriving as a zero. FromProto normalizes
	// both to the default page size a moment later, exactly as the HTTP path
	// does — but nothing normalizes the filter a Pagination reports, and
	// ToSQLArgs reads an explicit zero there as a request for no rows and an
	// absent one as a request for the default.
	if in.MaxResponseSize != nil {
		qf.SetMaxResponseSize(uint64(in.GetMaxResponseSize()))
	}

	return qf, platformerrors.Join(errs...)
}

// ToProto converts a QueryFilter into the wire message.
//
// A nil filter crosses as an absent message rather than as the default one,
// which is where this parts company with ToValues. A url.Values has no way to
// say "no filter", so ToValues writes the defaults out; a message can simply
// not be there, and FromProto reads an absent one as the default filter. Nil
// therefore survives the round trip as the same request, and a Pagination that
// reports no applied filter keeps reporting none rather than acquiring one it
// never applied.
//
// Absent fields stay absent rather than crossing as zeroes: every field on the
// message has explicit presence for that reason, and a page size of zero that
// was set is not the same value as one that was never set.
func ToProto(qf *filtering.QueryFilter) *filteringpb.QueryFilter {
	if qf == nil {
		return nil
	}

	// The pointers are copied rather than shared. SortAscending and
	// SortDescending are package-level pointers every filter in the process
	// points at, so handing one to a message would put a write through
	// out.SortBy in a position to change what "asc" means everywhere.
	out := &filteringpb.QueryFilter{}

	if qf.SortBy != nil {
		out.SortBy = new(*qf.SortBy)
	}

	if qf.Cursor != nil {
		out.Cursor = new(*qf.Cursor)
	}

	if qf.IncludeArchived != nil {
		out.IncludeArchived = new(*qf.IncludeArchived)
	}

	if qf.CreatedAfter != nil {
		out.CreatedAfter = timestamppb.New(*qf.CreatedAfter)
	}

	if qf.CreatedBefore != nil {
		out.CreatedBefore = timestamppb.New(*qf.CreatedBefore)
	}

	if qf.UpdatedAfter != nil {
		out.UpdatedAfter = timestamppb.New(*qf.UpdatedAfter)
	}

	if qf.UpdatedBefore != nil {
		out.UpdatedBefore = timestamppb.New(*qf.UpdatedBefore)
	}

	if qf.MaxResponseSize != nil {
		out.MaxResponseSize = new(uint32(*qf.MaxResponseSize))
	}

	return out
}

// PaginationFromProto converts the response half back.
//
// The applied filter is copied across without being normalized: it is the
// report of what a page was answered under, and defaulting it here would
// describe a page that was never served. Its page size is still clamped before
// it narrows, because that is a property of the types rather than a policy —
// a uint32 that wraps into a uint16 is wrong whichever direction it travels.
//
// A nil message converts to the zero Pagination, which is the first page of
// nothing with its counts unanswered — and CountsKnown false is what says so.
func PaginationFromProto(in *filteringpb.Pagination) (filtering.Pagination, error) {
	if in == nil {
		return filtering.Pagination{}, nil
	}

	out := filtering.Pagination{
		Cursor:          in.GetCursor(),
		PreviousCursor:  in.GetPreviousCursor(),
		FilteredCount:   in.GetFilteredCount(),
		TotalCount:      in.GetTotalCount(),
		MaxResponseSize: filtering.ClampResponseSize(uint64(in.GetMaxResponseSize())),
		CountsKnown:     in.GetCountsKnown(),
	}

	applied := in.GetAppliedQueryFilter()
	if applied == nil {
		return out, nil
	}

	qf, err := fromProto(applied)
	out.AppliedQueryFilter = qf

	if err != nil {
		return out, platformerrors.Wrapf(err, "reading the %s field", fieldAppliedFilter)
	}

	return out, nil
}

// PaginationToProto converts the response half onto the wire.
//
// The counts cross as they are, with counts_known alongside them, because the
// pair is meaningless without it: a store that could not answer them reports 0
// and 0, which is also what an empty collection reports, and the flag is the
// only thing that tells a client which of those it received.
func PaginationToProto(p filtering.Pagination) *filteringpb.Pagination {
	return &filteringpb.Pagination{
		AppliedQueryFilter: ToProto(p.AppliedQueryFilter),
		Cursor:             p.Cursor,
		PreviousCursor:     p.PreviousCursor,
		FilteredCount:      p.FilteredCount,
		TotalCount:         p.TotalCount,
		MaxResponseSize:    uint32(p.MaxResponseSize),
		CountsKnown:        p.CountsKnown,
	}
}
