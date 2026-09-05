package waitlists

import (
	"context"

	"github.com/primandproper/platform-go/v14/database"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/identifiers"
	"github.com/primandproper/platform-go/v14/observability"
	"github.com/primandproper/platform-go/v14/tenancy"
	"github.com/primandproper/platform-go/v14/waitlists/internal/waitlistsdb"
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

	var created *List

	if err := s.client.WithTransaction(ctx, func(q database.Tx) (err error) {
		created, err = s.createList(ctx, op, q, scope, list)

		return err
	}); err != nil {
		return nil, op.Error(err, "creating waitlist")
	}

	return created, nil
}

// CreateListTx is CreateList inside the caller's transaction, so the list and
// whatever the caller records about opening it commit together or not at all.
// See [Store].
func (s *SQLStore) CreateListTx(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	list *List,
) (*List, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if q == nil {
		return nil, op.Error(ErrNilExecutor, "creating waitlist")
	}

	created, err := s.createList(ctx, op, q, scope, list)
	if err != nil {
		return nil, op.Error(err, "creating waitlist")
	}

	return created, nil
}

// createList is the shared body of CreateList and CreateListTx.
//
// It takes the executor rather than reaching for one, which is the whole of the
// difference between the two: every check, every statement, and the order they
// run in are the same on both paths, so neither can drift into accepting a list
// the other refuses. The errors it returns are unwrapped; each caller reports
// them once, against its own span.
func (s *SQLStore) createList(
	ctx context.Context,
	op observability.Operation,
	q waitlistsdb.DBTX,
	scope tenancy.Scope,
	list *List,
) (*List, error) {
	if list == nil {
		return nil, ErrNilList
	}

	if err := scope.Validate(); err != nil {
		return nil, err
	}

	if err := list.validate(); err != nil {
		return nil, platformerrors.Wrapf(err, "creating waitlist %q", list.Name)
	}

	created := *list
	created.Scope = scope
	created.ClosesAt = created.ClosesAt.UTC()

	if created.ID == "" {
		created.ID = identifiers.New()
	}

	op.Set(listKey, created.ID)

	if err := s.q.CreateList(ctx, q, createListParams(&created, scope)); err != nil {
		return nil, platformerrors.Wrapf(err, "creating waitlist %q", created.ID)
	}

	row, err := s.q.GetListCreatedAt(ctx, q, waitlistsdb.GetListCreatedAtParams{ID: created.ID})
	if err != nil {
		return nil, platformerrors.Wrap(err, "reading back the waitlist's creation time")
	}

	created.CreatedAt = row.CreatedAt.UTC()

	// Nothing has edited or archived a list that was written a moment ago,
	// and a caller who filled either in is a caller describing a row that does
	// not exist yet.
	created.LastUpdatedAt = nil
	created.ArchivedAt = nil

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

	return op.Error(s.updateList(ctx, op, s.client.Writer(), scope, list), "updating waitlist")
}

// UpdateListTx is UpdateList inside the caller's transaction. See [Store].
func (s *SQLStore) UpdateListTx(ctx context.Context, q database.Tx, scope tenancy.Scope, list *List) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "updating waitlist")
	}

	return op.Error(s.updateList(ctx, op, q, scope, list), "updating waitlist")
}

// updateList is the shared body of UpdateList and UpdateListTx, which differ in
// the executor they run on and in nothing else.
func (s *SQLStore) updateList(
	ctx context.Context,
	op observability.Operation,
	q waitlistsdb.DBTX,
	scope tenancy.Scope,
	list *List,
) error {
	if list == nil {
		return ErrNilList
	}

	op.Set(listKey, list.ID)

	if err := scope.Validate(); err != nil {
		return err
	}

	if err := requireID(list.ID); err != nil {
		return err
	}

	if err := list.validate(); err != nil {
		return platformerrors.Wrapf(err, "updating waitlist %q", list.ID)
	}

	count, err := s.q.UpdateList(ctx, q, updateListParams(list, scope))

	return guardCount(count, err, ErrListNotFound, "updating waitlist")
}

// ArchiveList retires one of the scope's lists.
func (s *SQLStore) ArchiveList(ctx context.Context, scope tenancy.Scope, listID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
	)
	defer op.End()

	return op.Error(s.archiveList(ctx, s.client.Writer(), scope, listID), "archiving waitlist %q", listID)
}

// ArchiveListTx is ArchiveList inside the caller's transaction. See [Store].
func (s *SQLStore) ArchiveListTx(ctx context.Context, q database.Tx, scope tenancy.Scope, listID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(listKey, listID),
	)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "archiving waitlist %q", listID)
	}

	return op.Error(s.archiveList(ctx, q, scope, listID), "archiving waitlist %q", listID)
}

// archiveList is the shared body of ArchiveList and ArchiveListTx, which differ
// in the executor they run on and in nothing else.
func (s *SQLStore) archiveList(
	ctx context.Context,
	q waitlistsdb.DBTX,
	scope tenancy.Scope,
	listID string,
) error {
	if err := scope.Validate(); err != nil {
		return err
	}

	count, err := s.q.ArchiveList(ctx, q, waitlistsdb.ArchiveListParams{ID: listID, Scope: scope})

	return guardCount(count, err, ErrListNotFound, "archiving waitlist")
}

// readList is the read by id, through whatever executor the caller is holding.
//
// It is the read Join makes inside its transaction as well as the one GetList
// makes outside one, which is what puts the "is this list open" decision on the
// same row the signup is written against.
func (s *SQLStore) readList(
	ctx context.Context,
	q waitlistsdb.DBTX,
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
