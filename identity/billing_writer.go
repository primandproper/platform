package identity

import (
	"context"

	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// The SQLStore's BillingWriter: one method, because a processor webhook is
// unauthenticated and public and what it can reach is worth being able to state
// in one line.
var _ BillingWriter = (*SQLStore)(nil)

// UpdateAccountBilling writes only the billing fields the update names.
func (s *SQLStore) UpdateAccountBilling(ctx context.Context, scope tenancy.Scope, accountID string, update *BillingUpdate) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(accountIDKey, accountID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return op.Error(err, "updating identity account billing")
	}

	if err := update.ValidateWithContext(ctx); err != nil {
		return op.Error(err, "updating identity account billing")
	}

	query, args := s.tables.buildUpdateAccountBilling(s.dialect, scope, accountID, update, s.now())

	if err := s.execExpectingRow(ctx, op, s.client.Writer(), query, args, ErrAccountNotFound, "updating identity account billing"); err != nil {
		return op.Error(err, "updating identity account billing")
	}

	return nil
}
