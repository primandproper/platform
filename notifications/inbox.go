package notifications

import (
	"context"

	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/identifiers"
	"github.com/primandproper/platform-go/v14/notifications/internal/notificationsdb"
	"github.com/primandproper/platform-go/v14/observability"
	"github.com/primandproper/platform-go/v14/tenancy"
)

// The SQLStore's Inbox: what somebody was told, and whether they have read it.
var _ Inbox = (*SQLStore)(nil)

// CreateNotification files one notification and reads back the creation time
// the database assigned.
//
// The read-back is a second round trip on a write path, and it is worth it:
// created_at is database-owned — see notifications/internal/queries — so the
// insert does not carry it, and the alternative is a value whose CreatedAt says
// 0001-01-01 for a row written a moment ago. A service that serializes what it
// just created straight into a response would render that as a date rather than
// as an absence.
func (s *SQLStore) CreateNotification(ctx context.Context, notification *Notification) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if notification == nil {
		return op.Error(ErrNilNotification, "creating notification")
	}

	op.Set(scopeKey, notification.Scope.String()).Set(principalKey, notification.Principal)

	if err := validNotification(notification); err != nil {
		return op.Error(err, "creating notification")
	}

	if notification.ID == "" {
		notification.ID = identifiers.New()
	}

	op.Set(notificationIDKey, notification.ID)

	if err := s.q.CreateNotification(ctx, s.client.Writer(),
		createNotificationParams(notification)); err != nil {
		return op.Error(err, "creating notification")
	}

	created, err := s.q.GetNotificationCreatedAt(ctx, s.client.Writer(),
		notificationsdb.GetNotificationCreatedAtParams{
			ID:        notification.ID,
			Scope:     notification.Scope,
			Principal: notification.Principal,
		})
	if err != nil {
		return op.Error(err, "reading back the notification's creation time")
	}

	notification.CreatedAt = created.CreatedAt.UTC()

	return nil
}

// GetNotification reads one of the principal's live notifications.
func (s *SQLStore) GetNotification(
	ctx context.Context,
	scope tenancy.Scope,
	principal, notificationID string,
) (*Notification, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(principalKey, principal),
		observability.WithValue(notificationIDKey, notificationID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading notification %q", notificationID)
	}

	if principal == "" {
		return nil, op.Error(ErrEmptyPrincipal, "reading notification %q", notificationID)
	}

	row, err := s.q.GetNotification(ctx, s.client.Reader(), notificationsdb.GetNotificationParams{
		ID:        notificationID,
		Scope:     scope,
		Principal: principal,
	})
	if err != nil {
		return nil, op.Error(notFound(err, ErrNotificationNotFound), "reading notification %q", notificationID)
	}

	return notificationFromRow(&row), nil
}

// ListNotifications pages the principal's inbox, in the direction the filter
// names.
func (s *SQLStore) ListNotifications(
	ctx context.Context,
	scope tenancy.Scope,
	principal string,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Notification], error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(principalKey, principal),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing notifications")
	}

	if principal == "" {
		return nil, op.Error(ErrEmptyPrincipal, "listing notifications")
	}

	filter = pageFilter(filter)

	listRows, err := sortedRows(filter,
		func() ([]notificationsdb.ListNotificationsRow, error) {
			return s.q.ListNotifications(ctx, s.client.Reader(),
				listNotificationsParams(scope, principal, filter))
		},
		func() ([]notificationsdb.ListNotificationsDescendingRow, error) {
			return s.q.ListNotificationsDescending(ctx, s.client.Reader(),
				notificationsdb.ListNotificationsDescendingParams(
					listNotificationsParams(scope, principal, filter)))
		},
		func(r notificationsdb.ListNotificationsDescendingRow) notificationsdb.ListNotificationsRow {
			return notificationsdb.ListNotificationsRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing notifications")
	}

	rows := make([]pageRow[Notification], 0, len(listRows))
	for i := range listRows {
		rows = append(rows, notificationPageRow(&listRows[i]))
	}

	op.SpanOnly(countKey, len(rows))

	return drainNotifications(rows, filter), nil
}

// ListUnreadNotifications is ListNotifications restricted to what the principal
// has not read.
//
// The count a caller wants beside the page — the badge number — is the result's
// filtered count, which the statement carries on its rows: it is of everything
// unread rather than of the page, so a client asking for one notification learns
// how many there are.
func (s *SQLStore) ListUnreadNotifications(
	ctx context.Context,
	scope tenancy.Scope,
	principal string,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Notification], error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(principalKey, principal),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing unread notifications")
	}

	if principal == "" {
		return nil, op.Error(ErrEmptyPrincipal, "listing unread notifications")
	}

	filter = pageFilter(filter)

	params := func() notificationsdb.ListUnreadNotificationsParams {
		return notificationsdb.ListUnreadNotificationsParams(
			listNotificationsParams(scope, principal, filter))
	}

	listRows, err := sortedRows(filter,
		func() ([]notificationsdb.ListUnreadNotificationsRow, error) {
			return s.q.ListUnreadNotifications(ctx, s.client.Reader(), params())
		},
		func() ([]notificationsdb.ListUnreadNotificationsDescendingRow, error) {
			return s.q.ListUnreadNotificationsDescending(ctx, s.client.Reader(),
				notificationsdb.ListUnreadNotificationsDescendingParams(params()))
		},
		func(r notificationsdb.ListUnreadNotificationsDescendingRow) notificationsdb.ListUnreadNotificationsRow {
			return notificationsdb.ListUnreadNotificationsRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing unread notifications")
	}

	rows := make([]pageRow[Notification], 0, len(listRows))
	for i := range listRows {
		rows = append(rows, unreadPageRow(&listRows[i]))
	}

	op.SpanOnly(countKey, len(rows))

	return drainNotifications(rows, filter), nil
}

// drainNotifications is the one place a page of notifications becomes a result.
//
// The cursor is the id, because both statements order by it. A cursor naming a
// position in an order the query does not use is a page that skips rows and
// repeats others, with nothing reporting an error — so the two lists share this
// rather than each naming the field they page by.
func drainNotifications(
	rows []pageRow[Notification],
	filter *filtering.QueryFilter,
) *filtering.QueryFilteredResult[Notification] {
	return filtering.Drain(rows, pageValue, pageCounts,
		func(n *Notification) string { return n.ID }, filter)
}

// MarkNotificationRead stamps one notification as read, now.
//
// The statement guards on the stamp being absent, so a second mark matches
// nothing and the time the principal first read it survives. That leaves zero
// rows meaning two things — already read, or not in the inbox at all — and they
// are different answers, so the miss is disambiguated with a read rather than
// collapsed. It costs a round trip only on the path that already did nothing.
func (s *SQLStore) MarkNotificationRead(
	ctx context.Context,
	scope tenancy.Scope,
	principal, notificationID string,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(principalKey, principal),
		observability.WithValue(notificationIDKey, notificationID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "marking notification %q read", notificationID)
	}

	if principal == "" {
		return op.Error(ErrEmptyPrincipal, "marking notification %q read", notificationID)
	}

	readAt := s.now()

	count, err := s.q.MarkNotificationRead(ctx, s.client.Writer(),
		notificationsdb.MarkNotificationReadParams{
			ReadAt:    &readAt,
			ID:        notificationID,
			Scope:     scope,
			Principal: principal,
		})
	if err != nil {
		return op.Error(err, "marking notification %q read", notificationID)
	}

	if count > 0 {
		return nil
	}

	// Nothing was written. Either it was already read — in which case this is
	// the success the caller asked for — or the notification is not in this
	// principal's inbox, which is the error they need.
	if _, err = s.GetNotification(ctx, scope, principal, notificationID); err != nil {
		return op.Error(err, "marking notification %q read", notificationID)
	}

	return nil
}

// MarkAllNotificationsRead stamps everything the principal has not read, and
// reports how many that was.
func (s *SQLStore) MarkAllNotificationsRead(
	ctx context.Context,
	scope tenancy.Scope,
	principal string,
) (int64, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(principalKey, principal),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return 0, op.Error(err, "marking every notification read")
	}

	if principal == "" {
		return 0, op.Error(ErrEmptyPrincipal, "marking every notification read")
	}

	readAt := s.now()

	count, err := s.q.MarkAllNotificationsRead(ctx, s.client.Writer(),
		notificationsdb.MarkAllNotificationsReadParams{
			ReadAt:    &readAt,
			Scope:     scope,
			Principal: principal,
		})
	if err != nil {
		return 0, op.Error(err, "marking every notification read")
	}

	op.SpanOnly(countKey, count)

	return count, nil
}

// ArchiveNotification dismisses one notification.
//
// Zero rows is ErrNotificationNotFound rather than a quiet success, and the
// reading is exact: the statement excludes archived rows, so a notification that
// has already been dismissed is not in the inbox, which is what this method
// addresses.
func (s *SQLStore) ArchiveNotification(
	ctx context.Context,
	scope tenancy.Scope,
	principal, notificationID string,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(principalKey, principal),
		observability.WithValue(notificationIDKey, notificationID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "archiving notification %q", notificationID)
	}

	if principal == "" {
		return op.Error(ErrEmptyPrincipal, "archiving notification %q", notificationID)
	}

	count, err := s.q.ArchiveNotification(ctx, s.client.Writer(),
		notificationsdb.ArchiveNotificationParams{
			ID:        notificationID,
			Scope:     scope,
			Principal: principal,
		})

	return op.Error(
		guardCount(count, err, ErrNotificationNotFound, "archiving the notification"),
		"archiving notification %q", notificationID)
}

// validNotification is what the inbox requires of a row before it stores one.
//
// Three checks, and each refuses a row that would be unreachable rather than
// merely odd: a notification addressed to nobody is one no list can find, one
// with no topic is one no client can decide what to do with, and one with no
// title is one a client renders as a blank line.
func validNotification(n *Notification) error {
	if err := n.Scope.Validate(); err != nil {
		return err
	}

	if n.Principal == "" {
		return ErrEmptyPrincipal
	}

	if n.Topic == "" {
		return ErrEmptyTopic
	}

	if n.Title == "" {
		return platformerrors.Wrap(platformerrors.ErrEmptyInputProvided, "empty notification title")
	}

	return nil
}
