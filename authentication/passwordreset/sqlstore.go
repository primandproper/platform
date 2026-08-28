package passwordreset

import (
	"context"
	"database/sql"
	stderrors "errors"
	"time"

	"github.com/primandproper/platform-go/v13/authentication/passwordreset/migrations"
	"github.com/primandproper/platform-go/v13/clock"
	"github.com/primandproper/platform-go/v13/cryptography/hashing"
	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/observability/metrics"
	"github.com/primandproper/platform-go/v13/random"
	"github.com/primandproper/platform-go/v13/tenancy"
)

// serviceName names the loggers, spans, and instruments this store emits.
const serviceName = "password_reset"

// The keys this store records against a span and a log line.
//
// None of them is the secret, and none of them can be turned into it. What a
// trace shows is which row, whose reset, and which scope — enough to follow one
// password reset through a system, and not enough to complete somebody else's.
const (
	tokenKey = "password_reset.token_id"
	userKey  = "password_reset.user_id"
	scopeKey = "password_reset.scope"
)

var _ Store = (*SQLStore)(nil)

// SQLStore keeps password reset tokens in a SQL table, against the schema
// passwordreset/migrations renders.
//
// It is exported, and returned by NewSQLStore, so a caller who has chosen SQL
// storage can depend on that choice rather than on the Store seam. It does two
// things more than Store describes — Sweep removes rows past their deadline, and
// Digest renders the column a stored token is found by — and neither belongs on
// the interface: a store backed by something that expires its own entries needs
// no sweep, and a store that is not a table has no column.
type SQLStore struct {
	db        database.Client
	clock     clock.Clock
	generator random.Generator
	hasher    hashing.Hasher
	o11y      observability.Observer

	sweptCounter       metrics.Int64Counter
	sweepErrorsCounter metrics.Int64Counter

	table       string
	dialect     dialect.Dialect
	secretBytes int
}

// NewSQLStore builds a SQLStore over a database client.
//
// Reads go through the write pool, deliberately. A reset token is written by
// the request that asks for one and read by the very next request the user
// makes — the one they made by following a link that arrived seconds later —
// and replica lag turns that into a reset link that is "not found" and then
// works when reloaded. These rows are small, single-key, and live for an hour;
// they are not the reads worth scaling out.
//
// It does not create the table. Hand migrations.SQL to your own migration run.
func NewSQLStore(cfg *Config, db database.Client, opts ...Option) (*SQLStore, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}

	if db == nil {
		return nil, ErrNilDatabaseClient
	}

	d := db.Dialect()
	if !d.Valid() {
		return nil, platformerrors.Wrapf(dialect.ErrUnsupported, "password reset store dialect %q", d)
	}

	if err := migrations.ValidatePrefix(cfg.TablePrefix); err != nil {
		return nil, err
	}

	o := newOptions(opts)

	s := &SQLStore{
		db:          db,
		clock:       o.clock,
		generator:   o.generator,
		hasher:      o.hasher,
		table:       tableName(cfg.TablePrefix),
		dialect:     d,
		secretBytes: o.secretBytes,
		o11y:        observability.NewObserver(serviceName, o.logger, o.tracerProvider),
	}

	var err error
	if s.sweptCounter, s.sweepErrorsCounter, err = newSweepInstruments(o.metricsProvider); err != nil {
		return nil, err
	}

	if o.sweepCtx != nil {
		go s.sweepEvery(o.sweepCtx, o.sweepInterval)
	}

	return s, nil
}

// Digest renders what the token_digest column holds for a secret.
//
// It is exported for the one caller the Store seam cannot serve: a deployment
// migrating off a hand-written table, which has to write the new column from
// tokens it is holding, and a test asserting that the raw value is nowhere in
// the row. It is not a verification — comparing its output to a column by hand
// is how the single-use guarantee gets reimplemented badly — and it is not
// reversible, so a caller holding one of these holds nothing.
func (s *SQLStore) Digest(secret string) string {
	return hashing.HexString(s.hasher, secret)
}

// Issue mints a token for a principal, stores its digest, and returns the secret
// exactly once.
func (s *SQLStore) Issue(ctx context.Context, scope tenancy.Scope, userID string, ttl time.Duration) (*Issuance, error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return nil, err
	}

	if userID == "" {
		return nil, ErrEmptyUserID
	}

	if ttl <= 0 {
		return nil, ErrNonPositiveLifetime
	}

	op.SetValues(map[string]any{userKey: userID, scopeKey: scope.String()})

	secret, err := s.generator.GenerateBase64EncodedString(ctx, s.secretBytes)
	if err != nil {
		return nil, op.Error(err, "generating password reset token")
	}

	now := s.clock.Now().UTC()

	token := &Token{
		ID:        identifiers.New(),
		Scope:     scope,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}

	query, args := buildInsert(s.dialect, s.table, token, s.Digest(secret))

	if _, err = s.db.Writer().ExecContext(ctx, query, args...); err != nil {
		return nil, op.Error(err, "storing password reset token row")
	}

	op.Set(tokenKey, token.ID)

	return &Issuance{Token: token, Secret: secret}, nil
}

// Verify resolves a secret to its token without spending it.
func (s *SQLStore) Verify(ctx context.Context, scope tenancy.Scope, secret string) (*Token, error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if err := s.validateLookup(scope, secret); err != nil {
		return nil, err
	}

	op.Set(scopeKey, scope.String())

	token, err := s.read(ctx, s.db.Writer(), scope, secret)
	if err != nil {
		if isTokenError(err) {
			return nil, err
		}

		return nil, op.Error(err, "reading password reset token row")
	}

	observe(op, token)

	if err = s.liveness(token); err != nil {
		return nil, err
	}

	return token, nil
}

// Consume spends a secret, atomically, and returns the token it spent.
//
// The read and the redemption are one transaction, and it is the redemption
// that decides the answer. Two requests answering the same link at the same
// instant both read the row live; the second one's update finds redeemed_at
// already set and reports no rows, so exactly one of them is handed the token
// and the other is told it has been spent. A read that decided, with an update
// afterwards, would hand it to both — and the window would be exactly as wide as
// the password write that follows.
func (s *SQLStore) Consume(ctx context.Context, scope tenancy.Scope, secret string) (*Token, error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if err := s.validateLookup(scope, secret); err != nil {
		return nil, err
	}

	op.Set(scopeKey, scope.String())

	var token *Token

	if err := s.db.WithTransaction(ctx, func(q database.Tx) error {
		var txErr error
		token, txErr = s.redeem(ctx, q, scope, secret)

		return txErr
	}); err != nil {
		if isTokenError(err) {
			return nil, err
		}

		return nil, op.Error(err, "consuming password reset token")
	}

	observe(op, token)

	return token, nil
}

// RevokeForUser destroys every unredeemed token a principal holds.
func (s *SQLStore) RevokeForUser(ctx context.Context, scope tenancy.Scope, userID string) (int64, error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if err := scope.Validate(); err != nil {
		return 0, err
	}

	if userID == "" {
		return 0, ErrEmptyUserID
	}

	op.SetValues(map[string]any{userKey: userID, scopeKey: scope.String()})

	query, args := buildRevokeForUser(s.dialect, s.table, scope, userID)

	result, err := s.db.Writer().ExecContext(ctx, query, args...)
	if err != nil {
		return 0, op.Error(err, "revoking password reset token rows")
	}

	revoked, err := result.RowsAffected()
	if err != nil {
		return 0, op.Error(err, "counting revoked password reset token rows")
	}

	return revoked, nil
}

// redeem reads a token within one transaction and stamps its redemption,
// reporting ErrTokenRedeemed when the stamp finds nothing to write.
func (s *SQLStore) redeem(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	secret string,
) (*Token, error) {
	token, err := s.read(ctx, q, scope, secret)
	if err != nil {
		return nil, err
	}

	// Checked before the write rather than folded into its predicate. The
	// alternative is a guard comparing expires_at against the server's clock,
	// which would make the boundary a different comparison on each of the three
	// engines and would collapse "expired" and "already redeemed" into one
	// affected-row count of zero.
	if err = s.liveness(token); err != nil {
		return nil, err
	}

	at := s.clock.Now().UTC()

	query, args := buildRedeem(s.dialect, s.table, token.ID, at)

	result, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, platformerrors.Wrap(err, "redeeming password reset token row")
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, platformerrors.Wrap(err, "counting redeemed password reset token rows")
	}

	if affected == 0 {
		return nil, ErrTokenRedeemed
	}

	token.RedeemedAt = &at

	return token, nil
}

// read resolves a secret to its row, reporting ErrTokenNotFound when the digest
// matches nothing in scope.
func (s *SQLStore) read(
	ctx context.Context,
	q database.SQLQueryExecutor,
	scope tenancy.Scope,
	secret string,
) (*Token, error) {
	query, args := buildSelectByDigest(s.dialect, s.table, s.Digest(secret), scope)

	var (
		token     Token
		expiresAt any
		redeemed  any
		createdAt any
	)

	if err := q.QueryRowContext(ctx, query, args...).Scan(
		&token.ID, &token.Scope, &token.UserID, &expiresAt, &redeemed, &createdAt,
	); err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, ErrTokenNotFound
		}

		return nil, platformerrors.Wrap(err, "reading password reset token row")
	}

	if at, ok := database.CoerceTime(expiresAt); ok {
		token.ExpiresAt = at.UTC()
	}

	if at, ok := database.CoerceTime(createdAt); ok {
		token.CreatedAt = at.UTC()
	}

	if at, ok := database.CoerceTime(redeemed); ok {
		utc := at.UTC()
		token.RedeemedAt = &utc
	}

	return &token, nil
}

// liveness reports why a token cannot be spent, or nil when it can.
//
// The boundary itself is Token.Live's, deliberately, and this only names which
// half of it failed: a store comparing the deadline here as well would be a
// second copy of the comparison an administrative view renders from, free to
// disagree with it about the last second of a link's life.
//
// Redemption is reported before expiry, so a link somebody used yesterday says
// so rather than reporting the deadline it has since passed — which is the more
// useful of the two answers and the one a support conversation turns on.
func (s *SQLStore) liveness(token *Token) error {
	if token.Live(s.clock.Now()) {
		return nil
	}

	if token.RedeemedAt != nil {
		return ErrTokenRedeemed
	}

	return ErrTokenExpired
}

// validateLookup rejects a call that named no scope or no token before it
// reaches the database.
func (s *SQLStore) validateLookup(scope tenancy.Scope, secret string) error {
	if err := scope.Validate(); err != nil {
		return err
	}

	if secret == "" {
		return ErrEmptySecret
	}

	return nil
}

// observe records which row and whose reset an operation touched. The secret is
// not among them, and neither is its digest.
func observe(op observability.Operation, token *Token) {
	op.SetValues(map[string]any{tokenKey: token.ID, userKey: token.UserID})
}

// isTokenError reports whether err is one of the three outcomes a caller reads
// rather than an outcome an operator does.
//
// They are returned bare rather than through op.Error because none of them is a
// failure: a user following an expired link is the flow working. Marking those
// spans as errors would put the most routine event in this package at the top of
// an error dashboard, and would bury the driver failures that belong there.
func isTokenError(err error) bool {
	return stderrors.Is(err, ErrTokenNotFound) ||
		stderrors.Is(err, ErrTokenExpired) ||
		stderrors.Is(err, ErrTokenRedeemed)
}
