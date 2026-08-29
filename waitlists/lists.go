package waitlists

import (
	"context"

	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
	"github.com/primandproper/platform-go/v13/waitlists/internal/waitlistsdb"
)

// The SQLStore's ListStore: the catalog, written by whoever administers a
// deployment and read by everything else.
var _ ListStore = (*SQLStore)(nil)

// CreateList opens a waitlist in the scope's catalog.
//
// The insert and the read-back of the creation time share one transaction. The
// column is the database's — see waitlists/internal/queries — so the alternative
// is a caller whose struct says 0001-01-01 for a row written a moment ago.
func (s *SQLStore) CreateList(ctx context.Context, scope tenancy.Scope, list *List) (*List, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if list == nil {
		return nil, op.Error(ErrNilList, "creating waitlist")
	}

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "creating waitlist %q", list.Name)
	}

	if err := list.validate(); err != nil {
		return nil, op.Error(err, "creating waitlist %q", list.Name)
	}

	created := *list
	created.Scope = scope
	created.ClosesAt = created.ClosesAt.UTC()

	if created.ID == "" {
		created.ID = identifiers.New()
	}

	op.Set(listKey, created.ID)

	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		if err := s.q.CreateList(ctx, q, createListParams(&created, scope)); err != nil {
			return platformerrors.Wrap(err, "creating waitlist")
		}

		row, err := s.q.GetListCreatedAt(ctx, q, waitlistsdb.GetListCreatedAtParams{ID: created.ID})
		if err != nil {
			return platformerrors.Wrap(err, "reading back the waitlist's creation time")
		}

		created.CreatedAt = row.CreatedAt.UTC()

		return nil
	}); err != nil {
		return nil, op.Error(err, "creating waitlist %q", list.Name)
	}

	return &created, nil
}

// GetList reads one of the scope's live lists by id.
func (s *SQLStore) GetList(ctx context.Context, scope tenancy.Scope, listID string) (*List, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading waitlist %q", listID)
	}

	list, err := s.readList(ctx, s.client.Reader(), scope, listID)
	if err != nil {
		return nil, op.Error(err, "reading waitlist %q", listID)
	}

	return list, nil
}

// ListLists pages the scope's catalog, in the direction the filter names.
//
// The direction is a choice between two generated statements rather than an
// argument either of them binds — see sortedRows — so what this method does with
// filter.SortBy is pick the one whose ORDER BY and cursor comparison agree with
// it.
func (s *SQLStore) ListLists(
	ctx context.Context,
	scope tenancy.Scope,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[List], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing waitlists")
	}

	filter = pageFilter(filter)

	listRows, err := sortedRows(filter,
		func() ([]waitlistsdb.ListListsRow, error) {
			return s.q.ListLists(ctx, s.client.Reader(), listListsParams(scope, filter))
		},
		func() ([]waitlistsdb.ListListsDescendingRow, error) {
			return s.q.ListListsDescending(ctx, s.client.Reader(),
				waitlistsdb.ListListsDescendingParams(listListsParams(scope, filter)))
		},
		func(r waitlistsdb.ListListsDescendingRow) waitlistsdb.ListListsRow {
			return waitlistsdb.ListListsRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing waitlists")
	}

	rows := make([]pageRow[List], 0, len(listRows))
	for i := range listRows {
		rows = append(rows, listPageRow(&listRows[i]))
	}

	op.SpanOnly(countKey, len(rows))

	// The cursor is the id, because the statement orders by it. A cursor naming
	// a position in an order the query does not use is a page that skips rows
	// and repeats others, with nothing reporting an error.
	return filtering.Drain(rows, pageValue, pageCounts,
		func(l *List) string { return l.ID }, filter), nil
}

// ListOpenLists pages the lists still taking signups.
//
// The horizon is the store's clock rather than the server's, bound as an
// argument — see queries.OpenAsOfArg. That is what puts this read and
// List.OpenAt on the same clock: a test that moves the store's clock past a
// list's closing time sees the list leave this page, which it would not if the
// statement read CURRENT_TIMESTAMP.
func (s *SQLStore) ListOpenLists(
	ctx context.Context,
	scope tenancy.Scope,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[List], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing open waitlists")
	}

	filter = pageFilter(filter)
	asOf := s.clock.Now().UTC()

	listRows, err := sortedRows(filter,
		func() ([]waitlistsdb.ListOpenListsRow, error) {
			return s.q.ListOpenLists(ctx, s.client.Reader(), listOpenListsParams(scope, asOf, filter))
		},
		func() ([]waitlistsdb.ListOpenListsDescendingRow, error) {
			return s.q.ListOpenListsDescending(ctx, s.client.Reader(),
				waitlistsdb.ListOpenListsDescendingParams(listOpenListsParams(scope, asOf, filter)))
		},
		func(r waitlistsdb.ListOpenListsDescendingRow) waitlistsdb.ListOpenListsRow {
			return waitlistsdb.ListOpenListsRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing open waitlists")
	}

	rows := make([]pageRow[List], 0, len(listRows))
	for i := range listRows {
		rows = append(rows, openListPageRow(&listRows[i]))
	}

	op.SpanOnly(countKey, len(rows))

	return filtering.Drain(rows, pageValue, pageCounts,
		func(l *List) string { return l.ID }, filter), nil
}

// UpdateList rewrites a list's name, description and closing time.
func (s *SQLStore) UpdateList(ctx context.Context, scope tenancy.Scope, list *List) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if list == nil {
		return op.Error(ErrNilList, "updating waitlist")
	}

	op.Set(listKey, list.ID)

	if err := scope.Validate(); err != nil {
		return op.Error(err, "updating waitlist %q", list.ID)
	}

	if err := requireID(list.ID); err != nil {
		return op.Error(err, "updating waitlist %q", list.ID)
	}

	if err := list.validate(); err != nil {
		return op.Error(err, "updating waitlist %q", list.ID)
	}

	count, err := s.q.UpdateList(ctx, s.client.Writer(), updateListParams(list, scope))
	if err = guardCount(count, err, ErrListNotFound, "updating waitlist"); err != nil {
		return op.Error(err, "updating waitlist %q", list.ID)
	}

	return nil
}

// ArchiveList retires one of the scope's lists.
func (s *SQLStore) ArchiveList(ctx context.Context, scope tenancy.Scope, listID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "archiving waitlist %q", listID)
	}

	count, err := s.q.ArchiveList(ctx, s.client.Writer(),
		waitlistsdb.ArchiveListParams{ID: listID, Scope: scope})
	if err = guardCount(count, err, ErrListNotFound, "archiving waitlist"); err != nil {
		return op.Error(err, "archiving waitlist %q", listID)
	}

	return nil
}

// readList is the read by id, through whatever executor the caller is holding.
//
// It is the read Join makes inside its transaction as well as the one GetList
// makes outside one, which is what puts the "is this list open" decision on the
// same row the signup is written against.
func (s *SQLStore) readList(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	listID string,
) (*List, error) {
	if err := requireID(listID); err != nil {
		return nil, err
	}

	row, err := s.q.GetList(ctx, q, waitlistsdb.GetListParams{ID: listID, Scope: scope})
	if err != nil {
		return nil, notFound(err, ErrListNotFound)
	}

	return listFromRow(&row), nil
}
