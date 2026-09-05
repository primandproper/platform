package billing

import (
	"context"
	"time"

	"github.com/primandproper/platform-go/v14/capitalism"
	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/tenancy"
)

// Store is the whole of what this package persists: what a deployment sells,
// what each account has bought, and what happened when they paid for it.
//
// It is four interfaces because they have four callers, and the split is the
// useful part. A checkout path reaches a PurchaseStore, a webhook handler
// reaches the SubscriptionStore and the TransactionStore, an admin console
// reaches the ProductStore, and an entitlement check reaches one method on the
// SubscriptionStore. Splitting them is what lets a component depend on the part
// it uses; Store is here for the wiring that provides all four.
//
// Every write here has a second form, named for the executor it takes, that runs
// inside a transaction the caller owns. A payment provider's event is rarely one
// row: the audit entry naming what happened and the data change event on an
// outbox somebody fans out are its ordinary companions, and a companion written
// after this store's own transaction has committed is one that can go missing
// while the row stays. The package documentation says what that buys.
type Store interface {
	ProductStore
	SubscriptionStore
	PurchaseStore
	TransactionStore
}

// ProductStore is the catalog: what a deployment sells, at what price, on what
// recurrence.
//
// Every method takes a tenancy.Scope, and a deployment with one catalog passes
// tenancy.Global() to all of them. There is deliberately no unscoped variant of
// any of these — see the tenancy package.
type ProductStore interface {
	// CreateProduct adds a product to the scope's catalog and returns it as
	// stored, with the id it was minted under and the creation time the database
	// assigned.
	//
	// It refuses a product with no name, an unrecognized kind, a currency that
	// is not three characters, a negative price, a recurring product with no
	// billing interval, and a one-time product that has one. A provider-side id
	// already claimed in this scope returns an error wrapping ErrProductExists
	// rather than a driver's constraint violation.
	CreateProduct(ctx context.Context, scope tenancy.Scope, product *Product) (*Product, error)

	// CreateProductTx is CreateProduct inside the caller's transaction, so the
	// product commits with whatever the caller writes beside it. A nil q is an
	// error wrapping ErrNilExecutor.
	//
	// It exists because a row in a consumer's schema is rarely written alone.
	// An audit entry naming who stocked the catalog and a data change event on
	// an outbox somebody fans out are the ordinary companions, and a companion
	// is worth what its atomicity with the row is worth. Written after this
	// method's own transaction has committed, they are a window in which the
	// product exists and nothing downstream has been told — narrow,
	// one-directional, and still not something a consumer can close from
	// outside this package.
	//
	// Every check CreateProduct makes is made here, and every statement runs on
	// q: the insert-ignore, the read-back of the creation time, and the
	// attribution read the insert makes when it loses. A subscription opened
	// against this product later in the same transaction finds it.
	CreateProductTx(ctx context.Context, q database.Tx, scope tenancy.Scope, product *Product) (*Product, error)

	// GetProduct reads one live product by id.
	GetProduct(ctx context.Context, scope tenancy.Scope, productID string) (*Product, error)

	// GetProductByExternalID reads one live product by the payment provider's
	// identifier for it, which is the lookup a catalog sync makes.
	//
	// It refuses an empty identifier with ErrEmptyExternalID rather than
	// answering "not found": the column is NULL for a product with no provider
	// behind it, and a comparison against NULL matches nothing, so the honest
	// answer to an empty argument is that the caller did not supply one.
	GetProductByExternalID(ctx context.Context, scope tenancy.Scope, externalProductID string) (*Product, error)

	// ProductExists reports whether the scope has a live product by that id,
	// without reading it.
	//
	// It is the check a subscription or a purchase write makes before it
	// inserts — on whatever executor that write is running on, so the Tx
	// variants see a product their own transaction stocked — which is why this
	// is the one table here that keeps its existence query.
	ProductExists(ctx context.Context, scope tenancy.Scope, productID string) (bool, error)

	// ListProducts pages the scope's catalog.
	ListProducts(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Product], error)

	// UpdateProduct rewrites a product's name, description, kind, price,
	// currency, billing interval and provider-side id.
	//
	// Repricing a product changes what the next sale costs and nothing about
	// what anybody already paid — the amount on a purchase and on a ledger row
	// is that sale's own. What it will not do is revive an archived product.
	UpdateProduct(ctx context.Context, scope tenancy.Scope, product *Product) error

	// UpdateProductTx is UpdateProduct inside the caller's transaction, so the
	// edit commits with whatever the caller records about it. A nil q is an
	// error wrapping ErrNilExecutor. See CreateProductTx for the argument in
	// full.
	//
	// The collision check against the provider-side id runs on q, so a catalog
	// sync writing several products in one transaction is checked against what
	// that transaction has written rather than against what was committed
	// before it began.
	UpdateProductTx(ctx context.Context, q database.Tx, scope tenancy.Scope, product *Product) error

	// ArchiveProduct withdraws a product from sale.
	//
	// The subscriptions already on it keep renewing and the purchases already
	// made stay readable, because archiving is taking something off the shelf
	// rather than cancelling what has been sold. A deployment that means to end
	// the agreements ends them through the payment provider, and the statuses
	// arrive here as events.
	ArchiveProduct(ctx context.Context, scope tenancy.Scope, productID string) error

	// ArchiveProductTx is ArchiveProduct inside the caller's transaction, so
	// the withdrawal commits with whatever the caller records about it. A nil q
	// is an error wrapping ErrNilExecutor. See CreateProductTx for the argument
	// in full.
	ArchiveProductTx(ctx context.Context, q database.Tx, scope tenancy.Scope, productID string) error
}

// SubscriptionStore is the recurring half: who is paying for what, and until
// when.
type SubscriptionStore interface {
	// CreateSubscription opens an agreement and returns it as stored.
	//
	// A provider-side id already claimed in this scope returns an error wrapping
	// ErrSubscriptionExists, which is what a redelivered subscription webhook
	// gets — the uniqueness is what keeps one paying customer from ending up
	// with two agreements.
	CreateSubscription(ctx context.Context, scope tenancy.Scope, subscription *Subscription) (*Subscription, error)

	// CreateSubscriptionTx is CreateSubscription inside the caller's
	// transaction, so the agreement commits with whatever the caller writes
	// beside it. A nil q is an error wrapping ErrNilExecutor. See
	// CreateProductTx for the argument in full.
	//
	// It is the variant a webhook handler reaches for: the subscription, the
	// audit entry naming the event that opened it, and the data change event
	// the dispatcher fans out land together or not at all. A store owning its
	// own transaction leaves a window in which a subscription exists and an
	// entitlement check can read it while nothing has been told about it.
	//
	// Two reads move onto q with the write. The product check the create is
	// gated on runs there, so a product created through CreateProductTx earlier
	// in the same transaction is one a subscription can be opened against; and
	// so does the attribution read on the losing path, so a redelivery arriving
	// in the same transaction as the row it collides with is named rather than
	// mis-blamed.
	CreateSubscriptionTx(ctx context.Context, q database.Tx, scope tenancy.Scope, subscription *Subscription) (*Subscription, error)

	// GetSubscription reads one live subscription by id.
	GetSubscription(ctx context.Context, scope tenancy.Scope, subscriptionID string) (*Subscription, error)

	// GetSubscriptionByExternalID reads one live subscription by the payment
	// provider's identifier for it, which is the lookup every subscription
	// webhook begins with. It refuses an empty identifier — see
	// GetProductByExternalID.
	GetSubscriptionByExternalID(ctx context.Context, scope tenancy.Scope, externalSubscriptionID string) (*Subscription, error)

	// ListSubscriptions pages every subscription in the scope. It is the
	// administrative read.
	ListSubscriptions(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Subscription], error)

	// ListSubscriptionsForAccount pages one account's subscriptions, current and
	// lapsed alike. It is the customer-facing read, and a separate statement
	// rather than ListSubscriptions filtered afterwards, because a page filtered
	// after the fact is a page whose size the caller cannot rely on.
	ListSubscriptionsForAccount(ctx context.Context, scope tenancy.Scope, accountID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Subscription], error)

	// ListCurrentSubscriptions pages the account's subscriptions whose paid
	// period covers the store's clock.
	//
	// It is the read every entitlement check ultimately makes, and it
	// deliberately does not filter on the status: which reported status leaves
	// an account entitled is policy, it differs between deployments selling the
	// same thing, and capitalism's documentation is where that ruling lives. So
	// this answers the part that is a fact about the row, and the caller reads
	// Subscription.Status off what comes back. PlanSource is the worked example.
	ListCurrentSubscriptions(ctx context.Context, scope tenancy.Scope, accountID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Subscription], error)

	// UpdateSubscription rewrites the plan, the provider-side id, the status and
	// the paid period: everything a provider's own subscription can move.
	//
	// It is the sync write, and it assigns all of them together because this row
	// is a restatement of a fact the provider owns — a sync able to write half of
	// it would leave the row disagreeing with the truth in a way nothing reports.
	// What it does not touch is the account: moving a subscription between
	// accounts is not an edit, and a store that allowed it would let one
	// customer's payments settle another's bill.
	UpdateSubscription(ctx context.Context, scope tenancy.Scope, subscription *Subscription) error

	// UpdateSubscriptionTx is UpdateSubscription inside the caller's
	// transaction, so the sync commits with whatever the caller records about
	// it. A nil q is an error wrapping ErrNilExecutor. See CreateProductTx for
	// the argument in full.
	//
	// The collision check against the provider-side id runs on q, for the
	// reason UpdateProductTx's does.
	UpdateSubscriptionTx(ctx context.Context, q database.Tx, scope tenancy.Scope, subscription *Subscription) error

	// SetSubscriptionStatus moves the standing and nothing else.
	//
	// It is the narrow write for the event that carries only a status, and it is
	// guarded: the statement is `SET status = X WHERE status <> X`, so a
	// redelivered event finds the row already holding X, touches nothing, and is
	// reported as ErrStatusUnchanged. That is an answer rather than a failure —
	// a caller processing provider events acknowledges the delivery on it.
	SetSubscriptionStatus(ctx context.Context, scope tenancy.Scope, subscriptionID string, status capitalism.SubscriptionStatus) error

	// SetSubscriptionStatusTx is SetSubscriptionStatus inside the caller's
	// transaction, so the standing commits with whatever the caller records
	// about it. A nil q is an error wrapping ErrNilExecutor. See
	// CreateProductTx for the argument in full.
	//
	// The guard is unchanged and so is what a redelivery gets: the statement
	// runs on q, and the read that tells ErrStatusUnchanged from
	// ErrSubscriptionNotFound runs there too — so a status write against a
	// subscription this transaction opened is answered by the row it wrote
	// rather than by a snapshot that predates it.
	SetSubscriptionStatusTx(ctx context.Context, q database.Tx, scope tenancy.Scope, subscriptionID string, status capitalism.SubscriptionStatus) error

	// ArchiveSubscription retires a subscription administratively.
	//
	// It is not a cancellation and must not be used as one: a cancelled
	// subscription is one whose status says so, which is a fact the provider
	// reports, and archiving hides the row from every read that does not ask for
	// archived rows while changing nothing about what it holds. The ledger rows
	// pointing at it are left alone.
	ArchiveSubscription(ctx context.Context, scope tenancy.Scope, subscriptionID string) error

	// ArchiveSubscriptionTx is ArchiveSubscription inside the caller's
	// transaction, so the retirement commits with whatever the caller records
	// about it. A nil q is an error wrapping ErrNilExecutor. See
	// CreateProductTx for the argument in full.
	ArchiveSubscriptionTx(ctx context.Context, q database.Tx, scope tenancy.Scope, subscriptionID string) error
}

// PurchaseStore is the one-time half: what an account bought outright.
type PurchaseStore interface {
	// CreatePurchase records a sale and returns it as stored, outstanding.
	//
	// The row is written when the attempt starts rather than when it settles, so
	// that the transaction recording the attempt has something of ours to point
	// at. A provider-side transaction id already claimed in this scope returns an
	// error wrapping ErrPurchaseExists.
	CreatePurchase(ctx context.Context, scope tenancy.Scope, purchase *Purchase) (*Purchase, error)

	// CreatePurchaseTx is CreatePurchase inside the caller's transaction, so
	// the sale commits with whatever the caller writes beside it. A nil q is an
	// error wrapping ErrNilExecutor. See CreateProductTx for the argument in
	// full, and CreateSubscriptionTx for the two reads that move onto q with
	// the write — they are the same two here.
	CreatePurchaseTx(ctx context.Context, q database.Tx, scope tenancy.Scope, purchase *Purchase) (*Purchase, error)

	// GetPurchase reads one live purchase by id.
	GetPurchase(ctx context.Context, scope tenancy.Scope, purchaseID string) (*Purchase, error)

	// GetPurchaseByExternalID reads one live purchase by the payment provider's
	// identifier for the payment. It refuses an empty identifier — see
	// GetProductByExternalID.
	GetPurchaseByExternalID(ctx context.Context, scope tenancy.Scope, externalTransactionID string) (*Purchase, error)

	// ListPurchases pages every purchase in the scope. It is the administrative
	// read.
	ListPurchases(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Purchase], error)

	// ListPurchasesForAccount pages one account's purchases.
	ListPurchasesForAccount(ctx context.Context, scope tenancy.Scope, accountID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Purchase], error)

	// CompletePurchase stamps the moment the money arrived.
	//
	// at is the provider's own settlement time rather than this store's clock,
	// because a webhook can arrive hours after the payment it describes and
	// revenue is recognized against when the money moved. The zero time falls
	// back to the store's clock, for a completion that has no provider behind it
	// — a comped order, a migration.
	//
	// It is guarded on completed_at being NULL, so a purchase completes exactly
	// once and a redelivered event gets ErrAlreadyCompleted rather than restamping
	// the moment it settled.
	CompletePurchase(ctx context.Context, scope tenancy.Scope, purchaseID string, at time.Time) error

	// CompletePurchaseTx is CompletePurchase inside the caller's transaction,
	// so the settlement commits with whatever the caller records about it. A
	// nil q is an error wrapping ErrNilExecutor. See CreateProductTx for the
	// argument in full.
	//
	// The guard is unchanged and so is what a replay gets. Both the statement
	// and the read that tells ErrAlreadyCompleted from ErrPurchaseNotFound run
	// on q, so a sale created and settled in one transaction — a comped order,
	// a migration — is answered by the row that transaction wrote.
	CompletePurchaseTx(ctx context.Context, q database.Tx, scope tenancy.Scope, purchaseID string, at time.Time) error

	// ArchivePurchase retires a purchase administratively. It is not a refund —
	// a refund is a transaction of its own, recorded through the ledger.
	ArchivePurchase(ctx context.Context, scope tenancy.Scope, purchaseID string) error

	// ArchivePurchaseTx is ArchivePurchase inside the caller's transaction, so
	// the retirement commits with whatever the caller records about it. A nil q
	// is an error wrapping ErrNilExecutor. See CreateProductTx for the argument
	// in full.
	ArchivePurchaseTx(ctx context.Context, q database.Tx, scope tenancy.Scope, purchaseID string) error
}

// TransactionStore is the ledger: what each attempt to move money left behind.
type TransactionStore interface {
	// RecordTransaction writes one attempt and returns it as stored.
	//
	// It is named for what it is rather than Create, because a ledger row is a
	// record of something that happened elsewhere rather than a resource this
	// application decided to make.
	//
	// A provider-side id already recorded in this scope returns an error
	// wrapping ErrTransactionExists. That is the sentinel this table is shaped
	// around: payment providers redeliver, and a ledger that recorded one charge
	// twice is a number somebody reconciles by hand.
	RecordTransaction(ctx context.Context, scope tenancy.Scope, transaction *Transaction) (*Transaction, error)

	// RecordTransactionTx is RecordTransaction inside the caller's transaction,
	// so the ledger row commits with whatever the caller writes beside it. A
	// nil q is an error wrapping ErrNilExecutor. See CreateProductTx for the
	// argument in full.
	//
	// This is the write the argument is sharpest for. A charge arrives as a
	// webhook, and what a consumer records about it — who was billed, what the
	// dispatcher has to fan out — is written in the same breath; a ledger row
	// that committed ahead of its companions is a charge nothing downstream
	// heard about.
	//
	// The attribution the insert makes when it loses runs on q, which for this
	// table means the referent checks too: a ledger row naming a purchase
	// created earlier in the same transaction is attributed against a snapshot
	// that can see it, rather than refused for naming a row nobody has.
	//
	// The store's transactions instrument is incremented when the statement
	// writes the row rather than when the caller commits — nothing here can
	// observe somebody else's commit, and leaving this path uncounted would
	// remove the instrument entirely for whoever adopts it. A rolled back
	// transaction therefore leaves a count with no row behind it.
	RecordTransactionTx(ctx context.Context, q database.Tx, scope tenancy.Scope, transaction *Transaction) (*Transaction, error)

	// GetTransaction reads one live ledger row by id.
	GetTransaction(ctx context.Context, scope tenancy.Scope, transactionID string) (*Transaction, error)

	// GetTransactionByExternalID reads one live ledger row by the payment
	// provider's identifier for the attempt, which is how a handler tells a
	// redelivery from a new charge before it writes. It refuses an empty
	// identifier — see GetProductByExternalID.
	GetTransactionByExternalID(ctx context.Context, scope tenancy.Scope, externalTransactionID string) (*Transaction, error)

	// ListTransactions pages every ledger row in the scope. It is the
	// administrative read, and the one a reconciliation walks.
	ListTransactions(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Transaction], error)

	// ListTransactionsForAccount pages one account's ledger, oldest first by
	// default, which is the order the attempts were made in.
	ListTransactionsForAccount(ctx context.Context, scope tenancy.Scope, accountID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Transaction], error)

	// SetTransactionStatus moves an attempt's outcome: pending becoming
	// succeeded, succeeded becoming refunded.
	//
	// It is the only write this table has beyond the insert, and it is guarded
	// exactly as SetSubscriptionStatus is — a redelivered event gets
	// ErrStatusUnchanged. Everything else about a ledger row is a fact about
	// money that has already moved, and there is deliberately no statement able
	// to assign any of it.
	SetTransactionStatus(ctx context.Context, scope tenancy.Scope, transactionID string, status TransactionStatus) error

	// SetTransactionStatusTx is SetTransactionStatus inside the caller's
	// transaction, so the outcome commits with whatever the caller records
	// about it. A nil q is an error wrapping ErrNilExecutor. See
	// CreateProductTx for the argument in full, and SetSubscriptionStatusTx for
	// the guard, which is the same one.
	//
	// The counter is incremented on the write rather than on the caller's
	// commit, for the reason RecordTransactionTx gives.
	SetTransactionStatusTx(ctx context.Context, q database.Tx, scope tenancy.Scope, transactionID string, status TransactionStatus) error

	// ArchiveTransaction retires a ledger row administratively.
	//
	// It exists for the row written in error — a test charge, a duplicate that
	// predates the uniqueness — and not for a refund, which is a transaction of
	// its own.
	ArchiveTransaction(ctx context.Context, scope tenancy.Scope, transactionID string) error

	// ArchiveTransactionTx is ArchiveTransaction inside the caller's
	// transaction, so the retirement commits with whatever the caller records
	// about it. A nil q is an error wrapping ErrNilExecutor. See
	// CreateProductTx for the argument in full.
	ArchiveTransactionTx(ctx context.Context, q database.Tx, scope tenancy.Scope, transactionID string) error
}
