package billing

import (
	"context"
	"database/sql"
	"errors"

	"github.com/primandproper/platform-go/v14/billing/internal/billingdb"
	"github.com/primandproper/platform-go/v14/database"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/identifiers"
	"github.com/primandproper/platform-go/v14/observability"
	"github.com/primandproper/platform-go/v14/tenancy"
)

// The SQLStore's ProductStore: the catalog, written by whoever administers a
// deployment and read by everything that sells.
var _ ProductStore = (*SQLStore)(nil)

// CreateProduct adds a product to the scope's catalog.
//
// The insert and the read-back of the creation time share one transaction. The
// insert decides the collision itself — it is an insert-ignore over the (scope,
// external id) unique index, so a provider product already in the catalog leaves
// the row that is there unchanged and reports a zero affected count. That is
// still not a caught constraint violation, for the reason every uniqueness in
// this module avoids being one: the caller's next move differs between "this
// provider product is already in the catalog" and "the database is unwell", and
// asking them to parse a SQLSTATE to find out is how that distinction gets
// skipped. What the count cannot say is which identifier it lost to, which is
// refuseProductCreate's one read on the losing path.
func (s *SQLStore) CreateProduct(ctx context.Context, scope tenancy.Scope, product *Product) (*Product, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	created, err := productToCreate(op, scope, product)
	if err != nil {
		return nil, err
	}

	if err = s.client.WithTransaction(ctx, func(q database.Tx) error {
		return s.insertProduct(ctx, q, scope, created)
	}); err != nil {
		return nil, op.Error(err, "creating product %q", created.Name)
	}

	return created, nil
}

// CreateProductTx is CreateProduct inside the caller's transaction.
//
// Every check CreateProduct makes is made here, and every statement runs on q
// rather than in a transaction of this store's own — the insert, the read-back
// of the creation time, and the attribution read the insert-ignore makes when it
// loses. That last one is why the executor has to be the caller's: a redelivery
// arriving in the same transaction as the row it collides with would otherwise
// be attributed against a snapshot that cannot see it. See [Store.CreateProductTx].
func (s *SQLStore) CreateProductTx(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	product *Product,
) (*Product, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if q == nil {
		return nil, op.Error(ErrNilExecutor, "creating product")
	}

	created, err := productToCreate(op, scope, product)
	if err != nil {
		return nil, err
	}

	if err = s.insertProduct(ctx, q, scope, created); err != nil {
		return nil, op.Error(err, "creating product %q", created.Name)
	}

	return created, nil
}

// productToCreate is the checks CreateProduct and CreateProductTx share, and the
// value they write: a copy of the caller's, under the scope, normalized, with
// its id minted where the caller left it empty.
//
// Shared rather than written twice so that the two paths cannot drift into
// accepting different products. It runs before any transaction is opened,
// because a product refused here is refused without a round trip.
func productToCreate(op observability.Operation, scope tenancy.Scope, product *Product) (*Product, error) {
	if product == nil {
		return nil, op.Error(ErrNilProduct, "creating product")
	}

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "creating product %q", product.Name)
	}

	created := *product
	created.Scope = scope
	created.normalize()

	if err := created.validate(); err != nil {
		return nil, op.Error(err, "creating product %q", product.Name)
	}

	if created.ID == "" {
		created.ID = identifiers.New()
	}

	op.Set(productKey, created.ID)

	return &created, nil
}

// insertProduct is the statements the create runs, on whatever executor the
// caller is holding: the insert-ignore, the attribution of a loss, and the
// read-back of the creation time onto created.
func (s *SQLStore) insertProduct(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	created *Product,
) error {
	count, err := s.q.CreateProduct(ctx, q, createProductParams(created, scope))
	if err != nil {
		return platformerrors.Wrap(err, "creating product")
	}

	if count == 0 {
		return s.refuseProductCreate(ctx, q, scope, created)
	}

	row, err := s.q.GetProductCreatedAt(ctx, q, billingdb.GetProductCreatedAtParams{ID: created.ID})
	if err != nil {
		return platformerrors.Wrap(err, "reading back the product's creation time")
	}

	created.CreatedAt = row.CreatedAt.UTC()

	return nil
}

// GetProduct reads one of the scope's live products by id.
func (s *SQLStore) GetProduct(ctx context.Context, scope tenancy.Scope, productID string) (*Product, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(productKey, productID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading product %q", productID)
	}

	product, err := s.readProduct(ctx, s.client.Reader(), scope, productID)
	if err != nil {
		return nil, op.Error(err, "reading product %q", productID)
	}

	return product, nil
}

// GetProductByExternalID reads one live product by the payment provider's
// identifier for it.
//
// The statement behind it sees archived rows, because it is also the collision
// check the writes run — see billing/internal/queries. This is the caller that
// decides an archived hit is not an answer.
func (s *SQLStore) GetProductByExternalID(
	ctx context.Context,
	scope tenancy.Scope,
	externalProductID string,
) (*Product, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "reading product by external id")
	}

	product, err := s.readProductByExternalID(ctx, s.client.Reader(), scope, externalProductID)
	if err != nil {
		return nil, op.Error(err, "reading product by external id")
	}

	if product.ArchivedAt != nil {
		return nil, op.Error(ErrProductNotFound, "reading product by external id")
	}

	op.Set(productKey, product.ID)

	return product, nil
}

// ProductExists reports whether the scope has a live product by that id.
func (s *SQLStore) ProductExists(ctx context.Context, scope tenancy.Scope, productID string) (bool, error) {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(productKey, productID),
	)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return false, op.Error(err, "checking product %q", productID)
	}

	if err := requireID(productID); err != nil {
		return false, op.Error(err, "checking product %q", productID)
	}

	row, err := s.q.CheckProductExistence(ctx, s.client.Reader(),
		billingdb.CheckProductExistenceParams{ID: productID, Scope: scope})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}

		return false, op.Error(err, "checking product %q", productID)
	}

	return row.Exists, nil
}

// ListProducts pages the scope's catalog, in the direction the filter names.
//
// The direction is a choice between two generated statements rather than an
// argument either of them binds — see sortedRows — so what this method does with
// filter.SortBy is pick the one whose ORDER BY and cursor comparison agree with
// it.
func (s *SQLStore) ListProducts(
	ctx context.Context,
	scope tenancy.Scope,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Product], error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "listing products")
	}

	filter = pageFilter(filter)

	productRows, err := sortedRows(filter,
		func() ([]billingdb.ListProductsRow, error) {
			return s.q.ListProducts(ctx, s.client.Reader(), listProductsParams(scope, filter))
		},
		func() ([]billingdb.ListProductsDescendingRow, error) {
			return s.q.ListProductsDescending(ctx, s.client.Reader(),
				billingdb.ListProductsDescendingParams(listProductsParams(scope, filter)))
		},
		func(r billingdb.ListProductsDescendingRow) billingdb.ListProductsRow {
			return billingdb.ListProductsRow(r)
		})
	if err != nil {
		return nil, op.Error(err, "listing products")
	}

	rows := make([]pageRow[Product], 0, len(productRows))
	for i := range productRows {
		rows = append(rows, productPageRow(&productRows[i]))
	}

	op.SpanOnly(countKey, len(rows))

	return drainPage(rows, func(p *Product) string { return p.ID }, filter), nil
}

// UpdateProduct rewrites everything about a product a caller may assign.
func (s *SQLStore) UpdateProduct(ctx context.Context, scope tenancy.Scope, product *Product) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	updated, err := productToUpdate(op, scope, product)
	if err != nil {
		return err
	}

	if err = s.client.WithTransaction(ctx, func(q database.Tx) error {
		return s.rewriteProduct(ctx, q, scope, updated)
	}); err != nil {
		return op.Error(err, "updating product %q", updated.ID)
	}

	return nil
}

// UpdateProductTx is UpdateProduct inside the caller's transaction.
//
// The same checks and the same statements, on q — including the collision check
// against the provider-side id, so a product created earlier in the same
// transaction is one this edit can be checked against. See [Store.UpdateProductTx].
func (s *SQLStore) UpdateProductTx(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	product *Product,
) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(scopeKey, scope.String()))
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "updating product")
	}

	updated, err := productToUpdate(op, scope, product)
	if err != nil {
		return err
	}

	if err = s.rewriteProduct(ctx, q, scope, updated); err != nil {
		return op.Error(err, "updating product %q", updated.ID)
	}

	return nil
}

// productToUpdate is the checks UpdateProduct and UpdateProductTx share, and the
// value they write. It runs before any transaction is opened, for the reason
// productToCreate does.
func productToUpdate(op observability.Operation, scope tenancy.Scope, product *Product) (*Product, error) {
	if product == nil {
		return nil, op.Error(ErrNilProduct, "updating product")
	}

	op.Set(productKey, product.ID)

	if err := scope.Validate(); err != nil {
		return nil, op.Error(err, "updating product %q", product.ID)
	}

	if err := requireID(product.ID); err != nil {
		return nil, op.Error(err, "updating product %q", product.ID)
	}

	updated := *product
	updated.normalize()

	if err := updated.validate(); err != nil {
		return nil, op.Error(err, "updating product %q", product.ID)
	}

	return &updated, nil
}

// rewriteProduct is the statements the update runs, on whatever executor the
// caller is holding: the collision check against the provider-side id, and the
// guarded write.
func (s *SQLStore) rewriteProduct(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	updated *Product,
) error {
	// The row being updated is excluded by its own id, which is what lets a
	// product keep the provider id it already holds. The statement projects the
	// whole row, so the exclusion is a comparison here rather than a second
	// argument the SQL has to carry.
	if err := s.ensureProductExternalIDFree(ctx, q, scope, updated.ExternalProductID, updated.ID); err != nil {
		return err
	}

	count, err := s.q.UpdateProduct(ctx, q, updateProductParams(updated, scope))

	return guardCount(count, err, ErrProductNotFound, "updating product")
}

// ArchiveProduct withdraws one of the scope's products from sale.
//
// It is one statement, so it runs on the writer rather than in a transaction of
// its own; ArchiveProductTx is the form that joins somebody else's.
func (s *SQLStore) ArchiveProduct(ctx context.Context, scope tenancy.Scope, productID string) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(productKey, productID),
	)
	defer op.End()

	return s.archiveProduct(ctx, op, s.client.Writer(), scope, productID)
}

// ArchiveProductTx is ArchiveProduct inside the caller's transaction. See
// [Store.ArchiveProductTx].
func (s *SQLStore) ArchiveProductTx(
	ctx context.Context,
	q database.Tx,
	scope tenancy.Scope,
	productID string,
) error {
	ctx, op := s.o11y.Begin(ctx,
		observability.WithValue(scopeKey, scope.String()),
		observability.WithValue(productKey, productID),
	)
	defer op.End()

	if q == nil {
		return op.Error(ErrNilExecutor, "archiving product %q", productID)
	}

	return s.archiveProduct(ctx, op, q, scope, productID)
}

// archiveProduct is the shared body of ArchiveProduct and ArchiveProductTx,
// which differ in the executor they run on and in nothing else.
func (s *SQLStore) archiveProduct(
	ctx context.Context,
	op observability.Operation,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	productID string,
) error {
	if err := scope.Validate(); err != nil {
		return op.Error(err, "archiving product %q", productID)
	}

	count, err := s.q.ArchiveProduct(ctx, q, billingdb.ArchiveProductParams{ID: productID, Scope: scope})
	if err = guardCount(count, err, ErrProductNotFound, "archiving product"); err != nil {
		return op.Error(err, "archiving product %q", productID)
	}

	return nil
}

// readProduct is the read by id, through whatever executor the caller is
// holding.
func (s *SQLStore) readProduct(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	productID string,
) (*Product, error) {
	if err := requireID(productID); err != nil {
		return nil, err
	}

	row, err := s.q.GetProduct(ctx, q, billingdb.GetProductParams{ID: productID, Scope: scope})
	if err != nil {
		return nil, notFound(err, ErrProductNotFound)
	}

	return productFromRow(&row), nil
}

// readProductByExternalID is the read keyed on a provider's identifier, through
// whatever executor the caller is holding. It sees archived rows; its callers
// decide what one means.
func (s *SQLStore) readProductByExternalID(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	externalProductID string,
) (*Product, error) {
	if externalProductID == "" {
		return nil, ErrEmptyExternalID
	}

	row, err := s.q.GetProductByExternalID(ctx, q, billingdb.GetProductByExternalIDParams{
		Scope:             scope,
		ExternalProductID: nullable(externalProductID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrProductNotFound
		}

		return nil, platformerrors.Wrap(err, "reading product by external id")
	}

	return productFromRow((*billingdb.GetProductRow)(&row)), nil
}

// refuseProductCreate says which identifier the create lost to. See refuseCreate.
func (s *SQLStore) refuseProductCreate(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	created *Product,
) error {
	return refuseCreate(created.ExternalProductID, created.ID, func() error {
		_, err := s.readProductByExternalID(ctx, q, scope, created.ExternalProductID)

		return err
	}, ErrProductNotFound, ErrProductExists, nil)
}

// ensureProductExternalIDFree reports whether a provider-side product id is
// available to an update in this scope, excluding the row it belongs to already.
//
// An empty identifier is always free: it is stored as NULL, and NULL repeats —
// which is the whole reason a product with no provider behind it is storable at
// all. See billing/migrations.
//
// The creates do not use it. Theirs is the insert-ignore, which decides the same
// question inside the statement and so has no window between deciding and
// writing; an update has no such spelling, and does not need one — a redelivered
// sync writes the row's own external id back to it, which the exceptID branch
// and the index both allow. What is left for this to refuse is two different
// rows genuinely claiming one provider identifier.
func (s *SQLStore) ensureProductExternalIDFree(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	externalProductID, exceptID string,
) error {
	if externalProductID == "" {
		return nil
	}

	existing, err := s.readProductByExternalID(ctx, q, scope, externalProductID)

	switch {
	case errors.Is(err, ErrProductNotFound):
		return nil
	case err != nil:
		return err
	case existing.ID == exceptID:
		return nil
	default:
		return platformerrors.Wrapf(ErrProductExists, "external product id %q", externalProductID)
	}
}
