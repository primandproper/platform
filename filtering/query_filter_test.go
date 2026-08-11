package filtering

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
	loggingnoop "github.com/primandproper/platform-go/v10/observability/logging/noop"
	textsearch "github.com/primandproper/platform-go/v10/search/text"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestDefaultQueryFilter(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		qf := DefaultQueryFilter()

		must.NotNil(t, qf)
		must.NotNil(t, qf.MaxResponseSize)
		test.EqOp(t, uint16(DefaultQueryFilterLimit), *qf.MaxResponseSize)
		must.NotNil(t, qf.SortBy)
		test.EqOp(t, SortAscending, qf.SortBy)
	})
}

func TestQueryFilter_AttachToLogger(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		logger := loggingnoop.NewLogger()

		qf := &QueryFilter{
			Cursor:          new(t.Name()),
			MaxResponseSize: new(uint16(MaxQueryFilterLimit)),
			CreatedAfter:    new(time.Now().Truncate(time.Second)),
			CreatedBefore:   new(time.Now().Truncate(time.Second)),
			UpdatedAfter:    new(time.Now().Truncate(time.Second)),
			UpdatedBefore:   new(time.Now().Truncate(time.Second)),
			SortBy:          SortDescending,
			IncludeArchived: new(true),
		}

		test.NotNil(t, qf.AttachToLogger(logger))
	})

	T.Run("with nil", func(t *testing.T) {
		t.Parallel()

		logger := loggingnoop.NewLogger()

		test.NotNil(t, (*QueryFilter)(nil).AttachToLogger(logger))
	})
}

func TestQueryFilter_FromParams(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		tt, err := time.Parse(time.RFC3339Nano, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339Nano))
		must.NoError(t, err)

		actual := &QueryFilter{}
		expected := &QueryFilter{
			Cursor:          new(t.Name()),
			MaxResponseSize: new(uint16(MaxQueryFilterLimit)),
			CreatedAfter:    new(tt),
			CreatedBefore:   new(tt),
			UpdatedAfter:    new(tt),
			UpdatedBefore:   new(tt),
			SortBy:          SortDescending,
			IncludeArchived: new(true),
		}

		exampleInput := url.Values{
			textsearch.QueryKeySearch: []string{t.Name()},
			QueryKeyCursor:            []string{*expected.Cursor},
			QueryKeyLimit:             []string{strconv.Itoa(int(*expected.MaxResponseSize))},
			QueryKeyCreatedBefore:     []string{expected.CreatedAfter.Format(time.RFC3339Nano)},
			QueryKeyCreatedAfter:      []string{expected.CreatedBefore.Format(time.RFC3339Nano)},
			QueryKeyUpdatedBefore:     []string{expected.UpdatedAfter.Format(time.RFC3339Nano)},
			QueryKeyUpdatedAfter:      []string{expected.UpdatedBefore.Format(time.RFC3339Nano)},
			QueryKeySortBy:            []string{*expected.SortBy},
			QueryKeyIncludeArchived:   []string{strconv.FormatBool(true)},
		}

		must.NoError(t, actual.FromParams(exampleInput))

		test.Eq(t, expected, actual)

		exampleInput[QueryKeySortBy] = []string{*SortAscending}

		must.NoError(t, actual.FromParams(exampleInput))
		test.EqOp(t, SortAscending, actual.SortBy)
	})
}

func TestQueryFilter_FromParams_parseFailures(T *testing.T) {
	T.Parallel()

	T.Run("an absent parameter is not a failure", func(t *testing.T) {
		t.Parallel()

		qf := DefaultQueryFilter()

		// Every key present and empty, which is what a client sending `?limit=`
		// produces. None of them is a value that failed to parse.
		must.NoError(t, qf.FromParams(url.Values{
			QueryKeyLimit:           []string{""},
			QueryKeyCreatedBefore:   []string{""},
			QueryKeyCreatedAfter:    []string{""},
			QueryKeyUpdatedBefore:   []string{""},
			QueryKeyUpdatedAfter:    []string{""},
			QueryKeyIncludeArchived: []string{""},
			QueryKeySortBy:          []string{""},
		}))

		test.Eq(t, DefaultQueryFilter(), qf)
	})

	unreadable := map[string]url.Values{
		"limit":           {QueryKeyLimit: []string{"fifty"}},
		"negative limit":  {QueryKeyLimit: []string{"-5"}},
		"createdBefore":   {QueryKeyCreatedBefore: []string{"yesterday"}},
		"createdAfter":    {QueryKeyCreatedAfter: []string{"yesterday"}},
		"updatedBefore":   {QueryKeyUpdatedBefore: []string{"yesterday"}},
		"updatedAfter":    {QueryKeyUpdatedAfter: []string{"yesterday"}},
		"includeArchived": {QueryKeyIncludeArchived: []string{"yes please"}},
		"sortBy":          {QueryKeySortBy: []string{"sideways"}},
	}

	for name, params := range unreadable {
		T.Run("reports an unreadable "+name, func(t *testing.T) {
			t.Parallel()

			err := DefaultQueryFilter().FromParams(params)
			must.Error(t, err)
			test.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
		})
	}

	T.Run("reports every unreadable parameter, not just the first", func(t *testing.T) {
		t.Parallel()

		err := DefaultQueryFilter().FromParams(url.Values{
			QueryKeyLimit:        []string{"fifty"},
			QueryKeyCreatedAfter: []string{"yesterday"},
			QueryKeySortBy:       []string{"sideways"},
		})

		must.Error(t, err)
		test.StrContains(t, err.Error(), QueryKeyLimit)
		test.StrContains(t, err.Error(), QueryKeyCreatedAfter)
		test.StrContains(t, err.Error(), QueryKeySortBy)
	})

	T.Run("applies what did parse alongside the failure", func(t *testing.T) {
		t.Parallel()

		qf := DefaultQueryFilter()

		err := qf.FromParams(url.Values{
			QueryKeyCursor: []string{"abc"},
			QueryKeyLimit:  []string{"fifty"},
			QueryKeySortBy: []string{*SortDescending},
		})

		must.Error(t, err)
		must.NotNil(t, qf.Cursor)
		test.EqOp(t, "abc", *qf.Cursor)
		test.EqOp(t, SortDescending, qf.SortBy)
	})

	T.Run("an over-large limit still clamps rather than failing", func(t *testing.T) {
		t.Parallel()

		qf := DefaultQueryFilter()

		must.NoError(t, qf.FromParams(url.Values{
			QueryKeyLimit: []string{strconv.Itoa(MaxQueryFilterLimit * 10)},
		}))

		must.NotNil(t, qf.MaxResponseSize)
		test.EqOp(t, uint16(MaxQueryFilterLimit), *qf.MaxResponseSize)
	})

	T.Run("ExtractQueryFilterFromRequest reports and still returns a usable filter", func(t *testing.T) {
		t.Parallel()

		req, reqErr := http.NewRequestWithContext(
			t.Context(), http.MethodGet, "https://verygoodsoftwarenotvirus.ru", http.NoBody)
		must.NoError(t, reqErr)

		req.URL.RawQuery = url.Values{QueryKeyLimit: []string{"fifty"}}.Encode()

		qf, err := ExtractQueryFilterFromRequest(req)
		must.Error(t, err)
		test.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
		test.Eq(t, DefaultQueryFilter(), qf)
	})
}

func TestQueryFilter_SetCursor(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		expected := t.Name()
		qf := &QueryFilter{}
		qf.SetCursor(&expected)

		test.EqOp(t, expected, *qf.Cursor)
	})

	T.Run("with nil", func(t *testing.T) {
		t.Parallel()

		original := t.Name()
		qf := &QueryFilter{Cursor: &original}
		qf.SetCursor(nil)

		test.EqOp(t, original, *qf.Cursor)
	})
}

func TestQueryFilter_ToValues(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		tt, err := time.Parse(time.RFC3339Nano, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339Nano))
		must.NoError(t, err)

		qf := &QueryFilter{
			Cursor:          new(t.Name()),
			MaxResponseSize: new(uint16(MaxQueryFilterLimit)),
			CreatedAfter:    new(tt),
			CreatedBefore:   new(tt),
			UpdatedAfter:    new(tt),
			UpdatedBefore:   new(tt),
			SortBy:          SortDescending,
			IncludeArchived: new(true),
		}

		expected := url.Values{
			QueryKeyCursor:          []string{*qf.Cursor},
			QueryKeyLimit:           []string{strconv.Itoa(int(*qf.MaxResponseSize))},
			QueryKeyCreatedBefore:   []string{qf.CreatedAfter.Format(time.RFC3339Nano)},
			QueryKeyCreatedAfter:    []string{qf.CreatedBefore.Format(time.RFC3339Nano)},
			QueryKeyUpdatedBefore:   []string{qf.UpdatedAfter.Format(time.RFC3339Nano)},
			QueryKeyUpdatedAfter:    []string{qf.UpdatedBefore.Format(time.RFC3339Nano)},
			QueryKeyIncludeArchived: []string{strconv.FormatBool(*qf.IncludeArchived)},
			QueryKeySortBy:          []string{*qf.SortBy},
		}

		actual := qf.ToValues()
		test.Eq(t, expected, actual)
	})

	T.Run("with nil", func(t *testing.T) {
		t.Parallel()
		qf := (*QueryFilter)(nil)
		expected := DefaultQueryFilter().ToValues()
		actual := qf.ToValues()
		test.Eq(t, expected, actual)
	})
}

func TestExtractQueryFilter(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		tt, err := time.Parse(time.RFC3339Nano, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339Nano))
		must.NoError(t, err)

		expected := &QueryFilter{
			Cursor:          new(t.Name()),
			MaxResponseSize: new(uint16(MaxQueryFilterLimit)),
			CreatedAfter:    new(tt),
			CreatedBefore:   new(tt),
			UpdatedAfter:    new(tt),
			UpdatedBefore:   new(tt),
			SortBy:          SortDescending,
		}
		exampleInput := url.Values{
			textsearch.QueryKeySearch: []string{t.Name()},
			QueryKeyCursor:            []string{*expected.Cursor},
			QueryKeyLimit:             []string{strconv.Itoa(int(*expected.MaxResponseSize))},
			QueryKeyCreatedBefore:     []string{expected.CreatedAfter.Format(time.RFC3339Nano)},
			QueryKeyCreatedAfter:      []string{expected.CreatedBefore.Format(time.RFC3339Nano)},
			QueryKeyUpdatedBefore:     []string{expected.UpdatedAfter.Format(time.RFC3339Nano)},
			QueryKeyUpdatedAfter:      []string{expected.UpdatedBefore.Format(time.RFC3339Nano)},
			QueryKeySortBy:            []string{*expected.SortBy},
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://verygoodsoftwarenotvirus.ru", http.NoBody)
		test.NoError(t, err)
		must.NotNil(t, req)

		req.URL.RawQuery = exampleInput.Encode()
		actual, err := ExtractQueryFilterFromRequest(req)
		test.NoError(t, err)
		test.Eq(t, expected, actual)
	})

	T.Run("with missing values", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		expected := &QueryFilter{
			Cursor:          new(t.Name()),
			MaxResponseSize: new(uint16(DefaultQueryFilterLimit)),
			SortBy:          SortAscending,
		}
		exampleInput := url.Values{
			QueryKeyCursor: []string{*expected.Cursor},
			QueryKeyLimit:  []string{"0"},
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://verygoodsoftwarenotvirus.ru", http.NoBody)
		test.NoError(t, err)
		must.NotNil(t, req)

		req.URL.RawQuery = exampleInput.Encode()
		actual, err := ExtractQueryFilterFromRequest(req)
		test.NoError(t, err)
		test.Eq(t, expected, actual)
	})
}

func TestQueryFilter_ToPagination(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		qf := &QueryFilter{
			Cursor:          new(t.Name()),
			MaxResponseSize: new(uint16(MaxQueryFilterLimit)),
		}

		expected := Pagination{
			Cursor:          *qf.Cursor,
			MaxResponseSize: *qf.MaxResponseSize,
		}

		actual := qf.ToPagination()
		test.Eq(t, expected, actual)
	})

	T.Run("with nil value", func(t *testing.T) {
		t.Parallel()

		qf := (*QueryFilter)(nil)

		actual := qf.ToPagination()
		test.NotNil(t, actual)
	})
}

func TestNewQueryFilteredResult(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		qf := &QueryFilter{
			Cursor:          new(t.Name()),
			MaxResponseSize: new(uint16(MaxQueryFilterLimit)),
		}

		data := []*string{new("a"), new("b")}
		filteredCount := uint64(len(data))
		totalCount := uint64(len(data))
		idExtractor := func(s *string) string { return *s }

		expected := &QueryFilteredResult[string]{
			Data: data,
			Pagination: Pagination{
				Cursor:             *data[1],
				PreviousCursor:     *qf.Cursor,
				MaxResponseSize:    *qf.MaxResponseSize,
				FilteredCount:      filteredCount,
				TotalCount:         totalCount,
				AppliedQueryFilter: qf,
			},
		}

		actual := NewQueryFilteredResult(data, filteredCount, totalCount, idExtractor, qf)
		test.Eq(t, expected, actual)
	})

	T.Run("with empty data", func(t *testing.T) {
		t.Parallel()

		qf := &QueryFilter{
			Cursor:          new(t.Name()),
			MaxResponseSize: new(uint16(MaxQueryFilterLimit)),
		}

		data := []*string{}
		filteredCount := uint64(0)
		totalCount := uint64(0)
		idExtractor := func(s *string) string { return *s }

		expected := &QueryFilteredResult[string]{
			Data: data,
			Pagination: Pagination{
				Cursor:             "",
				PreviousCursor:     *qf.Cursor,
				MaxResponseSize:    *qf.MaxResponseSize,
				FilteredCount:      filteredCount,
				TotalCount:         totalCount,
				AppliedQueryFilter: qf,
			},
		}

		actual := NewQueryFilteredResult(data, filteredCount, totalCount, idExtractor, qf)
		test.Eq(t, expected, actual)
	})

	T.Run("with no cursor", func(t *testing.T) {
		t.Parallel()

		qf := &QueryFilter{
			MaxResponseSize: new(uint16(MaxQueryFilterLimit)),
		}

		data := []*string{new("a"), new("b")}
		filteredCount := uint64(len(data))
		totalCount := uint64(len(data))
		idExtractor := func(s *string) string { return *s }

		expected := &QueryFilteredResult[string]{
			Data: data,
			Pagination: Pagination{
				Cursor:             *data[1],
				PreviousCursor:     "",
				MaxResponseSize:    *qf.MaxResponseSize,
				FilteredCount:      filteredCount,
				TotalCount:         totalCount,
				AppliedQueryFilter: qf,
			},
		}

		actual := NewQueryFilteredResult(data, filteredCount, totalCount, idExtractor, qf)
		test.Eq(t, expected, actual)
	})
}
