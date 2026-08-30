package billing

import (
	"context"
	"database/sql"
	"errors"

	"github.com/primandproper/platform-go/v13/billing/internal/billingdb"
	"github.com/primandproper/platform-go/v13/capitalism"
	"github.com/primandproper/platform-go/v13/database"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The SQLStore's SubscriptionStore: the recurring half, written mostly from a
// payment provider's events.
var _ SubscriptionStore = (*SQLStore)(nil)

// CreateSubscription opens an agreement in the scope.
func (s *SQLStore) CreateSubscription(
	ctx context.Context,
	scope tenancy.Scope,
	subscription *Subscription,
) (*Subscription, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if subscription == nil {
		return nil, op.Error(ErrNilSubscription, "creating subscription")
	}

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "creating subscription")
	}

	created := *subscription
	created.Scope = scope
	created.CurrentPeriodStart = created.CurrentPeriodStart.UTC()
	created.CurrentPeriodEnd = created.CurrentPeriodEnd.UTC()

	if err := created.validate(); err != nil {
		return nil, op.Error(err, "creating subscription")
	}

	if created.ID == "" {
		created.ID = identifiers.New()
	}

	op.Set(subscriptionKey, created.ID)
	op.Set(accountKey, created.BelongsToAccount)

	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		if err := s.requireProduct(ctx, q, scope, created.ProductID); err != nil {
			return err
		}

		count, err := s.q.CreateSubscription(ctx, q, createSubscriptionParams(&created, scope))
		if err != nil {
			return platformerrors.Wrap(err, "creating subscription")
		}

		if count == 0 {
			return s.refuseSubscriptionCreate(ctx, q, scope, &created)
		}

		row, err := s.q.GetSubscriptionCreatedAt(ctx, q,
			billingdb.GetSubscriptionCreatedAtParams{ID: created.ID})
		if err != nil {
			return platformerrors.Wrap(err, "reading back the subscription's creation time")
		}

		created.CreatedAt = row.CreatedAt.UTC()

		return nil
	}); err != nil {
		return nil, op.Error(err, "creating subscription")
	}

	return &created, nil
}

// GetSubscription reads one of the scope's live subscriptions by id.
func (s *SQLStore) GetSubscription(
	ctx context.Context,
	scope tenancy.Scope,
	subscriptionID string,
) (*Subscription, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(subscriptionKey, subscriptionID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading subscription %q", subscriptionID)
	}

	if err := requireID(subscriptionID); err != nil {
		return nil, op.Error(err, "reading subscription %q", subscriptionID)
	}

	row, err := s.q.GetSubscription(ctx, s.client.Reader(),
		billingdb.GetSubscriptionParams{ID: subscriptionID, Scope: scope})
	if err != nil {
		return nil, op.Error(notFound(err, ErrSubscriptionNotFound), "reading subscription %q", subscriptionID)
	}

	return subscriptionFromRow(&row), nil
}

// GetSubscriptionByExternalID reads one live subscription by the payment
// provider's identifier for it.
//
// The statement behind it sees archived rows, because it is also the collision
// check the writes run. This is the caller that decides an archived hit is not an
// answer.
func (s *SQLStore) GetSubscriptionByExternalID(
	ctx context.Context,
	scope tenancy.Scope,
	externalSubscriptionID string,
) (*Subscription, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading subscription by external id")
	}

	subscription, err := s.readSubscriptionByExternalID(ctx, s.client.Reader(), scope, externalSubscriptionID)
	if err != nil {
		return nil, op.Error(err, "reading subscription by external id")
	}

	if subscription.ArchivedAt != nil {
		return nil, op.Error(ErrSubscriptionNotFound, "reading subscription by external id")
	}

	op.Set(subscriptionKey, subscription.ID)

	return subscription, nil
}

// ListSubscriptions pages every subscription in the scope.
func (s *SQLStore) ListSubscriptions(
	ctx context.Context,
	scope tenancy.Scope,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Subscription], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing subscriptions")
	}

	filter = pageFilter(filter)

	subscriptionRows, err := sortedRows(filter,
		func() ([]billingdb.ListSubscriptionsRow, error) {
			return s.q.ListSubscriptions(ctx, s.client.Reader(), listSubscriptionsParams(scope, filter))
		},
		func() ([]billingdb.ListSubscriptionsDescendingRow, error) {
			return s.q.ListSubscriptionsDescending(ctx, s.client.Reader(),
				billingdb.ListSubscriptionsDescendingParams(listSubscriptionsParams(scope, filter)))
		},
		func(r billingdb.ListSubscriptionsDescendingRow) billingdb.ListSubscriptionsRow {
			return billingdb.ListSubscriptionsRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing subscriptions")
	}

	return s.drainSubscriptions(op, subscriptionRows, filter), nil
}

// ListSubscriptionsForAccount pages one account's subscriptions.
func (s *SQLStore) ListSubscriptionsForAccount(
	ctx context.Context,
	scope tenancy.Scope,
	accountID string,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Subscription], error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(accountKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing subscriptions for account %q", accountID)
	}

	if err := requireAccount(accountID); err != nil {
		return nil, op.Error(err, "listing subscriptions for account")
	}

	filter = pageFilter(filter)

	subscriptionRows, err := sortedRows(filter,
		func() ([]billingdb.ListSubscriptionsForAccountRow, error) {
			return s.q.ListSubscriptionsForAccount(ctx, s.client.Reader(),
				listSubscriptionsForAccountParams(scope, accountID, filter))
		},
		func() ([]billingdb.ListSubscriptionsForAccountDescendingRow, error) {
			return s.q.ListSubscriptionsForAccountDescending(ctx, s.client.Reader(),
				billingdb.ListSubscriptionsForAccountDescendingParams(
					listSubscriptionsForAccountParams(scope, accountID, filter)))
		},
		func(r billingdb.ListSubscriptionsForAccountDescendingRow) billingdb.ListSubscriptionsForAccountRow {
			return billingdb.ListSubscriptionsForAccountRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing subscriptions for account %q", accountID)
	}

	shaped := make([]billingdb.ListSubscriptionsRow, 0, len(subscriptionRows))
	for i := range subscriptionRows {
		shaped = append(shaped, billingdb.ListSubscriptionsRow(subscriptionRows[i]))
	}

	return s.drainSubscriptions(op, shaped, filter), nil
}

// ListCurrentSubscriptions pages the account's subscriptions whose paid period
// covers the store's clock.
//
// The horizon is bound rather than read off the server — see
// queries.CurrentAsOfArg. That is what puts this read and [Subscription.CurrentAt]
// on the same clock: a test that moves the store's clock past a period's end sees
// the subscription leave this page, which it would not if the statement read
// CURRENT_TIMESTAMP.
func (s *SQLStore) ListCurrentSubscriptions(
	ctx context.Context,
	scope tenancy.Scope,
	accountID string,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Subscription], error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(accountKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing current subscriptions for account %q", accountID)
	}

	if err := requireAccount(accountID); err != nil {
		return nil, op.Error(err, "listing current subscriptions for account")
	}

	filter = pageFilter(filter)
	asOf := s.clock.Now().UTC()

	subscriptionRows, err := sortedRows(filter,
		func() ([]billingdb.ListCurrentSubscriptionsRow, error) {
			return s.q.ListCurrentSubscriptions(ctx, s.client.Reader(),
				listCurrentSubscriptionsParams(scope, accountID, asOf, filter))
		},
		func() ([]billingdb.ListCurrentSubscriptionsDescendingRow, error) {
			return s.q.ListCurrentSubscriptionsDescending(ctx, s.client.Reader(),
				billingdb.ListCurrentSubscriptionsDescendingParams(
					listCurrentSubscriptionsParams(scope, accountID, asOf, filter)))
		},
		func(r billingdb.ListCurrentSubscriptionsDescendingRow) billingdb.ListCurrentSubscriptionsRow {
			return billingdb.ListCurrentSubscriptionsRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing current subscriptions for account %q", accountID)
	}

	shaped := make([]billingdb.ListSubscriptionsRow, 0, len(subscriptionRows))
	for i := range subscriptionRows {
		shaped = append(shaped, billingdb.ListSubscriptionsRow(subscriptionRows[i]))
	}

	return s.drainSubscriptions(op, shaped, filter), nil
}

// UpdateSubscription rewrites everything a provider's own subscription can move.
func (s *SQLStore) UpdateSubscription(
	ctx context.Context,
	scope tenancy.Scope,
	subscription *Subscription,
) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if subscription == nil {
		return op.Error(ErrNilSubscription, "updating subscription")
	}

	op.Set(subscriptionKey, subscription.ID)

	if err := scope.Validate(); err != nil {
		return op.Error(err, "updating subscription %q", subscription.ID)
	}

	if err := requireID(subscription.ID); err != nil {
		return op.Error(err, "updating subscription %q", subscription.ID)
	}

	updated := *subscription
	updated.CurrentPeriodStart = updated.CurrentPeriodStart.UTC()
	updated.CurrentPeriodEnd = updated.CurrentPeriodEnd.UTC()

	if err := updated.validate(); err != nil {
		return op.Error(err, "updating subscription %q", subscription.ID)
	}

	if err := s.client.WithTransaction(ctx, func(q database.Tx) error {
		if err := s.ensureSubscriptionExternalIDFree(ctx, q, scope, updated.ExternalSubscriptionID, updated.ID); err != nil {
			return err
		}

		count, err := s.q.UpdateSubscription(ctx, q, updateSubscriptionParams(&updated, scope))

		return guardCount(count, err, ErrSubscriptionNotFound, "updating subscription")
	}); err != nil {
		return op.Error(err, "updating subscription %q", subscription.ID)
	}

	return nil
}

// SetSubscriptionStatus moves the standing and nothing else.
//
// The guard is in the statement rather than in a read before it, so a
// redelivered event is answered by the affected-row count: the row already holds
// the status, nothing is written, and the caller is told ErrStatusUnchanged.
// Distinguishing that from a missing subscription takes one read, which is made
// only on the losing path and therefore never on the hot one.
func (s *SQLStore) SetSubscriptionStatus(
	ctx context.Context,
	scope tenancy.Scope,
	subscriptionID string,
	status capitalism.SubscriptionStatus,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(subscriptionKey, subscriptionID),
		observability.WithValue(statusKey, string(status)),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "setting subscription %q status", subscriptionID)
	}

	if err := requireID(subscriptionID); err != nil {
		return op.Error(err, "setting subscription %q status", subscriptionID)
	}

	if !status.Known() {
		return op.Error(platformerrors.Wrapf(ErrInvalidStatus, "%q", status),
			"setting subscription %q status", subscriptionID)
	}

	count, err := s.q.SetSubscriptionStatus(ctx, s.client.Writer(), billingdb.SetSubscriptionStatusParams{
		Status: string(status),
		ID:     subscriptionID,
		Scope:  scope,
	})
	if err != nil {
		return op.Error(platformerrors.Wrap(err, "setting subscription status"),
			"setting subscription %q status", subscriptionID)
	}

	if count == 0 {
		return op.Error(s.refuseStatusWrite(ctx, scope, subscriptionID),
			"setting subscription %q status", subscriptionID)
	}

	return nil
}

// ArchiveSubscription retires one of the scope's subscriptions administratively.
func (s *SQLStore) ArchiveSubscription(ctx context.Context, scope tenancy.Scope, subscriptionID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(subscriptionKey, subscriptionID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "archiving subscription %q", subscriptionID)
	}

	count, err := s.q.ArchiveSubscription(ctx, s.client.Writer(),
		billingdb.ArchiveSubscriptionParams{ID: subscriptionID, Scope: scope})
	if err = guardCount(count, err, ErrSubscriptionNotFound, "archiving subscription"); err != nil {
		return op.Error(err, "archiving subscription %q", subscriptionID)
	}

	return nil
}

// drainSubscriptions turns one list statement's rows into the paged result.
func (s *SQLStore) drainSubscriptions(
	op observability.Operation,
	rows []billingdb.ListSubscriptionsRow,
	filter *filtering.QueryFilter,
) *filtering.QueryFilteredResult[Subscription] {
	page := make([]pageRow[Subscription], 0, len(rows))
	for i := range rows {
		page = append(page, subscriptionPageRow(&rows[i]))
	}

	op.SpanOnly(countKey, len(page))

	return drainPage(page, func(sub *Subscription) string { return sub.ID }, filter)
}

// refuseStatusWrite reports why a status write touched nothing, having lost its
// guard.
//
// The guard cannot say which of the two happened — a row that is not there and a
// row already holding the status both report zero — and the difference matters:
// a redelivery is fine and a write against a subscription nobody has is not. So
// the read is made here, on the losing path only.
func (s *SQLStore) refuseStatusWrite(ctx context.Context, scope tenancy.Scope, subscriptionID string) error {
	if _, err := s.readSubscription(ctx, s.client.Reader(), scope, subscriptionID); err != nil {
		return err
	}

	return platformerrors.Wrapf(ErrStatusUnchanged, "subscription %q", subscriptionID)
}

// readSubscription is the read by id, through whatever executor the caller is
// holding.
func (s *SQLStore) readSubscription(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	subscriptionID string,
) (*Subscription, error) {
	row, err := s.q.GetSubscription(ctx, q,
		billingdb.GetSubscriptionParams{ID: subscriptionID, Scope: scope})
	if err != nil {
		return nil, notFound(err, ErrSubscriptionNotFound)
	}

	return subscriptionFromRow(&row), nil
}

// readSubscriptionByExternalID is the read keyed on a provider's identifier. It
// sees archived rows; its callers decide what one means.
func (s *SQLStore) readSubscriptionByExternalID(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	externalSubscriptionID string,
) (*Subscription, error) {
	if externalSubscriptionID == "" {
		return nil, ErrEmptyExternalID
	}

	row, err := s.q.GetSubscriptionByExternalID(ctx, q, billingdb.GetSubscriptionByExternalIDParams{
		Scope:                  scope,
		ExternalSubscriptionID: nullable(externalSubscriptionID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSubscriptionNotFound
		}

		return nil, platformerrors.Wrap(err, "reading subscription by external id")
	}

	return subscriptionFromRow((*billingdb.GetSubscriptionRow)(&row)), nil
}

// refuseSubscriptionCreate says which identifier the create lost to. See
// refuseCreate.
func (s *SQLStore) refuseSubscriptionCreate(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	created *Subscription,
) error {
	return refuseCreate(created.ExternalSubscriptionID, created.ID, func() error {
		_, err := s.readSubscriptionByExternalID(ctx, q, scope, created.ExternalSubscriptionID)

		return err
	}, ErrSubscriptionNotFound, ErrSubscriptionExists, nil)
}

// ensureSubscriptionExternalIDFree reports whether a provider-side subscription
// id is available to an update in this scope, excluding the row it belongs to
// already.
//
// An empty identifier is always free: it is stored as NULL, and NULL repeats —
// which is what makes a subscription granted by hand storable at all.
//
// Only the update asks. The create decides the same question inside its
// insert-ignore, where there is no window between deciding and writing — see
// ensureProductExternalIDFree for why the update neither has that spelling nor
// needs it.
func (s *SQLStore) ensureSubscriptionExternalIDFree(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	externalSubscriptionID, exceptID string,
) error {
	if externalSubscriptionID == "" {
		return nil
	}

	existing, err := s.readSubscriptionByExternalID(ctx, q, scope, externalSubscriptionID)

	switch {
	case errors.Is(err, ErrSubscriptionNotFound):
		return nil
	case err != nil:
		return err
	case existing.ID == exceptID:
		return nil
	default:
		return platformerrors.Wrapf(ErrSubscriptionExists, "external subscription id %q", externalSubscriptionID)
	}
}
