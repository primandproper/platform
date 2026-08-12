// Package filtering is the shared vocabulary for list queries: which slice of a
// collection a caller asked for, and which slice they got.
//
// A QueryFilter carries the request half — a cursor, a page size, a sort
// direction, created/updated time windows, and whether archived rows count — and
// round-trips through URL query parameters, so the same value is what a handler
// parses out of an *http.Request and what a client puts back on the wire. The
// query-parameter names are exported constants rather than string literals
// spelled out per handler, which is what keeps a client and a server agreeing on
// them. Pagination is the response half, and is what an API response embeds
// alongside its data to say what was applied and where the next page starts.
//
// It builds no SQL and touches no database. This package decides what a caller
// asked for; translating that into a query belongs to whatever store answers it.
//
// Page size is clamped rather than rejected: a request for more than
// MaxQueryFilterLimit gets MaxQueryFilterLimit, and an absent one gets
// DefaultQueryFilterLimit. A parameter that is present and unreadable is a
// different matter, and parsing reports it — every parameter is still attempted
// and everything that parsed is applied, so the filter is always usable, but the
// error is there because a mistyped filter that is silently dropped answers with
// a plausible-looking page that excludes nothing. The handler decides which of
// those it wants; the parse does not decide for it.
//
// A nil *QueryFilter is usable throughout: it renders as the default filter and
// says so when attached to a logger, so handlers need no nil check before
// passing one along.
package filtering

import (
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability/keys"
	"github.com/primandproper/platform-go/v10/observability/logging"
)

const (
	// sortAscendingString is the pre-determined Ascending sortType for external use.
	sortAscendingString = "asc"
	// sortDescendingString is the pre-determined Descending sortType for external use.
	sortDescendingString = "desc"
)

var (
	// SortAscending is the pre-determined Ascending string for external use.
	SortAscending = new(sortAscendingString)
	// SortDescending is the pre-determined Descending string for external use.
	SortDescending = new(sortDescendingString)
)

const (
	// MaxQueryFilterLimit is the maximum value for list queries. Anything larger
	// clamps to it.
	//
	// The field is uint16 rather than uint8 deliberately: at uint8 the ceiling
	// sat five above the limit, so `maxResponseSize: 300` was an unmarshal
	// error rather than the clamp every other over-limit value gets.
	MaxQueryFilterLimit = 250
	// DefaultQueryFilterLimit represents how many results we return in a response by default.
	DefaultQueryFilterLimit = 50

	// QueryKeySearchWithDatabase is the query param key to find search queries in requests.
	QueryKeySearchWithDatabase = "useDB"

	// QueryKeyLimit is the query param key to specify a limit in a query.
	QueryKeyLimit = "limit"
	// QueryKeyCursor is the query param key for specifying which cursor to use in a list query.
	QueryKeyCursor = "cursor"
	// QueryKeyCreatedBefore is the query param key for a creation time limit in a list query.
	QueryKeyCreatedBefore = "createdBefore"
	// QueryKeyCreatedAfter is the query param key for a creation time limit in a list query.
	QueryKeyCreatedAfter = "createdAfter"
	// QueryKeyUpdatedBefore is the query param key for an updated time limit in a list query.
	QueryKeyUpdatedBefore = "updatedBefore"
	// QueryKeyUpdatedAfter is the query param key for an updated time limit in a list query.
	QueryKeyUpdatedAfter = "updatedAfter"
	// QueryKeyIncludeArchived is the query param key for including archived results in a query.
	QueryKeyIncludeArchived = "includeArchived"
	// QueryKeySortBy is the query param key for sort order in a query.
	QueryKeySortBy = "sortBy"
)

type (
	// Pagination represents a pagination request.
	Pagination struct {
		_                  struct{}     `json:"-"`
		AppliedQueryFilter *QueryFilter `json:"appliedQueryFilter"`
		Cursor             string       `json:"cursor"`
		PreviousCursor     string       `json:"previousCursor"`
		FilteredCount      uint64       `json:"filteredCount"`
		TotalCount         uint64       `json:"totalCount"`
		MaxResponseSize    uint16       `json:"maxResponseSize"`
	}

	// QueryFilter represents all the filters a User could apply to a list query.
	QueryFilter struct {
		_ struct{} `json:"-"`

		SortBy          *string    `json:"sortBy,omitempty"`
		CreatedAfter    *time.Time `json:"createdAfter,omitempty"`
		CreatedBefore   *time.Time `json:"createdBefore,omitempty"`
		UpdatedAfter    *time.Time `json:"updatedAfter,omitempty"`
		UpdatedBefore   *time.Time `json:"updatedBefore,omitempty"`
		MaxResponseSize *uint16    `json:"maxResponseSize,omitempty"`
		IncludeArchived *bool      `json:"includeArchived,omitempty"`
		Cursor          *string    `json:"cursor,omitempty"`
	}

	QueryFilteredResult[T any] struct {
		_    struct{} `json:"-"`
		Data []*T     `json:"data"`
		Pagination
	}
)

// DefaultQueryFilter builds the default query filter.
func DefaultQueryFilter() *QueryFilter {
	return &QueryFilter{
		MaxResponseSize: new(uint16(DefaultQueryFilterLimit)),
		SortBy:          SortAscending,
	}
}

// AttachToLogger attaches a QueryFilter's values to a logging.Logger.
func (qf *QueryFilter) AttachToLogger(logger logging.Logger) logging.Logger {
	l := logging.EnsureLogger(logger).Clone()

	if qf == nil {
		return l.WithValue(keys.FilterIsNilKey, true)
	}

	if qf.Cursor != nil {
		l = l.WithValue(QueryKeyCursor, qf.Cursor)
	}

	if qf.MaxResponseSize != nil {
		l = l.WithValue(QueryKeyLimit, qf.MaxResponseSize)
	}

	if qf.SortBy != nil {
		l = l.WithValue(QueryKeySortBy, qf.SortBy)
	}

	if qf.CreatedBefore != nil {
		l = l.WithValue(QueryKeyCreatedBefore, qf.CreatedBefore)
	}

	if qf.CreatedAfter != nil {
		l = l.WithValue(QueryKeyCreatedAfter, qf.CreatedAfter)
	}

	if qf.UpdatedBefore != nil {
		l = l.WithValue(QueryKeyUpdatedBefore, qf.UpdatedBefore)
	}

	if qf.UpdatedAfter != nil {
		l = l.WithValue(QueryKeyUpdatedAfter, qf.UpdatedAfter)
	}

	return l
}

// FromParams overrides the core QueryFilter values with values retrieved from
// url.Params, reporting any parameter that was supplied and could not be read.
//
// An absent parameter is not a failure — the filter simply keeps whatever it
// already held. A parameter that is present and unreadable is, and that is the
// distinction the method exists to draw. It used to make no distinction at all:
// `limit=fifty` and `createdAfter=yesterday` parsed to an error that was
// discarded, and the caller got an unfiltered list that looked exactly like a
// filtered one with nothing excluded. The person who notices is whoever reconciles
// the numbers a week later.
//
// Every parameter is attempted, so one bad value does not hide the next; the
// returned error joins all of them. Whatever did parse is applied, which makes an
// ignored error behave as the old method did — but a caller reporting the failure
// to a client should discard the filter rather than list against a half-applied
// one.
func (qf *QueryFilter) FromParams(params url.Values) error {
	var errs []error

	// unreadable names the parameter and the value that would not parse. The
	// value is the caller's own input coming back to them, and the transport
	// mappers answer ErrUnrecognizedInputValue with a constant message, so it
	// reaches logs and traces without reaching the response body.
	unreadable := func(key, value string, cause error) error {
		return platformerrors.Wrapf(
			platformerrors.Join(platformerrors.ErrUnrecognizedInputValue, cause),
			"reading %s parameter %q", key, value,
		)
	}

	// parseTime reads one RFC3339Nano parameter, recording an error only when the
	// parameter was supplied and would not parse.
	//
	// The absence check is not redundant with the parse: an absent parameter reads
	// as "", which time.Parse rejects by allocating a *time.ParseError. Four of
	// those per call, on every list request, for filters the overwhelming majority
	// of requests do not send — checking first costs a comparison and skips all of
	// it. It is also what keeps an unsent filter from being reported as an
	// unreadable one.
	parseTime := func(key string, into **time.Time) {
		raw := params.Get(key)
		if raw == "" {
			return
		}

		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			errs = append(errs, unreadable(key, raw, err))

			return
		}

		*into = &t
	}

	if i := params.Get(QueryKeyCursor); i != "" {
		qf.Cursor = &i
	}

	if raw := params.Get(QueryKeyLimit); raw != "" {
		i, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			errs = append(errs, unreadable(QueryKeyLimit, raw, err))
		} else {
			// Still clamped rather than rejected: MaxQueryFilterLimit documents an
			// over-large limit as a clamp, and a client asking for more than the
			// ceiling has asked a legible question with a legible answer.
			qf.MaxResponseSize = new(uint16(math.Min(math.Max(float64(i), 0), MaxQueryFilterLimit)))
		}
	}

	parseTime(QueryKeyCreatedBefore, &qf.CreatedBefore)
	parseTime(QueryKeyCreatedAfter, &qf.CreatedAfter)
	parseTime(QueryKeyUpdatedBefore, &qf.UpdatedBefore)
	parseTime(QueryKeyUpdatedAfter, &qf.UpdatedAfter)

	if raw := params.Get(QueryKeyIncludeArchived); raw != "" {
		i, err := strconv.ParseBool(raw)
		if err != nil {
			errs = append(errs, unreadable(QueryKeyIncludeArchived, raw, err))
		} else {
			qf.IncludeArchived = &i
		}
	}

	if raw := params.Get(QueryKeySortBy); raw != "" {
		switch strings.ToLower(raw) {
		case sortAscendingString:
			qf.SortBy = SortAscending
		case sortDescendingString:
			qf.SortBy = SortDescending
		default:
			// A sort order nobody recognized is the most expensive of these to
			// swallow: the request comes back sorted the other way, in full, and
			// looks entirely successful.
			errs = append(errs, platformerrors.Wrapf(platformerrors.ErrUnrecognizedInputValue,
				"reading %s parameter %q", QueryKeySortBy, raw))
		}
	}

	return platformerrors.Join(errs...)
}

// SetCursor sets the current page with certain constraints.
func (qf *QueryFilter) SetCursor(cursor *string) {
	if cursor != nil {
		qf.Cursor = cursor
	}
}

// ToValues returns a url.Values from a QueryFilter.
func (qf *QueryFilter) ToValues() url.Values {
	if qf == nil {
		return DefaultQueryFilter().ToValues()
	}

	v := url.Values{}

	if qf.Cursor != nil {
		v.Set(QueryKeyCursor, *qf.Cursor)
	}

	if qf.MaxResponseSize != nil {
		v.Set(QueryKeyLimit, strconv.FormatUint(uint64(*qf.MaxResponseSize), 10))
	}

	if qf.SortBy != nil {
		v.Set(QueryKeySortBy, *qf.SortBy)
	}

	if qf.CreatedBefore != nil {
		v.Set(QueryKeyCreatedBefore, qf.CreatedBefore.Format(time.RFC3339Nano))
	}

	if qf.CreatedAfter != nil {
		v.Set(QueryKeyCreatedAfter, qf.CreatedAfter.Format(time.RFC3339Nano))
	}

	if qf.UpdatedBefore != nil {
		v.Set(QueryKeyUpdatedBefore, qf.UpdatedBefore.Format(time.RFC3339Nano))
	}

	if qf.UpdatedAfter != nil {
		v.Set(QueryKeyUpdatedAfter, qf.UpdatedAfter.Format(time.RFC3339Nano))
	}

	if qf.IncludeArchived != nil {
		v.Set(QueryKeyIncludeArchived, strconv.FormatBool(*qf.IncludeArchived))
	}

	return v
}

// ToPagination returns a Pagination from a QueryFilter.
func (qf *QueryFilter) ToPagination() Pagination {
	if qf == nil {
		return DefaultQueryFilter().ToPagination()
	}

	x := Pagination{}

	if qf.Cursor != nil {
		x.Cursor = *qf.Cursor
	}

	if qf.MaxResponseSize != nil {
		x.MaxResponseSize = *qf.MaxResponseSize
	}

	return x
}

// ExtractQueryFilterFromRequest extracts a QueryFilter from a request,
// reporting any query parameter that was supplied and could not be read.
//
// The filter is always usable — it starts from DefaultQueryFilter and holds
// whatever parsed — so a handler that wants the old lenient behavior can log
// the error and list anyway. One that would rather not answer a mistyped filter
// with a plausible-looking page has an error wrapping
// errors.ErrUnrecognizedInputValue, which errors/http already renders as a 400.
func ExtractQueryFilterFromRequest(req *http.Request) (*QueryFilter, error) {
	qf := DefaultQueryFilter()
	err := qf.FromParams(req.URL.Query())

	if qf.MaxResponseSize != nil {
		if *qf.MaxResponseSize == 0 {
			qf.MaxResponseSize = new(uint16(DefaultQueryFilterLimit))
		}
	}

	return qf, err
}

// NewQueryFilteredResult creates a new QueryFilteredResult.
func NewQueryFilteredResult[T any](
	data []*T,
	filteredCount,
	totalCount uint64,
	idExtractor func(*T) string,
	filter *QueryFilter,
) *QueryFilteredResult[T] {
	x := &QueryFilteredResult[T]{
		Data:       data,
		Pagination: filter.ToPagination(),
	}

	x.FilteredCount = filteredCount
	x.TotalCount = totalCount
	x.AppliedQueryFilter = filter

	// Preserve the input cursor as PreviousCursor before overwriting with next cursor
	if filter != nil && filter.Cursor != nil {
		x.PreviousCursor = *filter.Cursor
	}

	if len(data) > 0 {
		x.Cursor = idExtractor(data[len(data)-1])
	} else {
		x.Cursor = ""
	}

	return x
}
