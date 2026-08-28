/*
Package identity stores the thing being authenticated.

This module ships every engine an application needs to authenticate somebody —
argon2 hashing, TOTP, WebAuthn, OAuth2, session management, token issuance,
authorization policy, tenancy — and until now shipped nothing to authenticate.
Every consumer supplied the User themselves, which meant every consumer wrote
the schema, the repository, the paging, the soft delete, the scoping, the
mocks, and the fakes. Measured in one of them that came to roughly eight
thousand hand-written non-test lines, and eighteen of the twenty fields on its
user type were fields every application has.

So this package owns the four things — users, accounts, memberships, invitations —
in the same way [github.com/primandproper/platform-go/v13/webhooks] owns
endpoints: a Store interface, a SQL implementation of it, the DDL for three
dialects, and a mock. A consumer keeps its service layer, its HTTP handlers,
its proto, and whatever columns are genuinely its own; it does not keep a
users table.

# Where the boundary with authentication falls

A credential lives here, on the user.

That is the load-bearing decision in this package and it is worth being
explicit about, because the obvious alternative — identity holds who somebody
is, authentication holds how they prove it — reads cleaner and is wrong for the
shape this module already has. The authentication subpackages are engines:
[github.com/primandproper/platform-go/v13/authentication/argon2] hashes and
compares, [github.com/primandproper/platform-go/v13/authentication/totp]
generates and validates, [github.com/primandproper/platform-go/v13/authentication/webauthn]
attests. None of them stores anything, none of them wants to, and giving one of
them a table would mean an application that hashes passwords in a different
package still ends up with a credential store it did not choose.

Splitting the row instead — user here, credential there — buys a tidier
diagram and costs a join on the single hottest read in any application, the one
that turns a username into something to compare a password against. It also
creates a second table with the same primary key, the same soft delete, and the
same scope, which two components would then have to keep consistent with each
other. A password hash is not a separate entity from the person; it is a column
on them.

What that means concretely: HashedPassword, RequiresPasswordChange,
PasswordLastChangedAt, TwoFactorSecret, TwoFactorSecretVerifiedAt, and the
email-address verification token are User fields, written and read through this
Store. The engines remain engines — this package never hashes, never compares,
never generates a TOTP secret. It stores what they produce, and
[User.Redacted] is how a user reaches a response body without them.

What is deliberately not here, and why it is not an omission: WebAuthn
credentials, password reset tokens, and sessions. Each is a set per user rather
than a column on one, each has a lifecycle of its own (a credential is
registered and revoked, a reset token is issued and burned, a session expires),
and each is consumed by exactly one engine. Their home is beside that engine —
the same rule that put the password hash here, applied to a fact that is not a
column. Sessions live in [github.com/primandproper/platform-go/v13/sessions],
WebAuthn credentials and ceremonies in
[github.com/primandproper/platform-go/v13/authentication/webauthn/database], and
password reset tokens in
[github.com/primandproper/platform-go/v13/authentication/passwordreset], which
also owns the two properties a consumer writing that table by hand gets wrong:
the token is stored as a digest, and single use is enforced by the store rather
than by whoever called it.

# Scope is not the account

Every row here carries a [github.com/primandproper/platform-go/v13/tenancy.Scope],
and every read filters on it — the module's rule, not an exception to it. The
scope is *not* the account. Accounts are rows in this schema; the scope is
whoever owns the directory those accounts and users live in.

For nearly every application that is [tenancy.Global], and the package then
behaves exactly as an unscoped one would: one directory, usernames unique
across it, accounts belonging to users who belong to accounts. An application
that runs several isolated directories out of one database — a reseller per
customer, a per-region deployment sharing a cluster, a staging tenant beside a
production one — names them with a scope, and a username is then unique within
a directory rather than across all of them.

The alternative was to make an account *be* a scope, which is tempting because
tenancy.Scope has always assumed an organization exists without being able to
name one. It is not what this package does. Doing it would mean an application
whose tenant is not an account — a workspace above accounts, a project below
them — could not use this store at all, and it would turn a consumer's
modeling choice into a compatibility promise this module then owes forever.
An account has a scope, like every other row in this module.

# Extending a user

A consumer with columns of its own puts them in a side table keyed by user ID,
and joins.

That is not a hedge, it is the whole value. The moment this package accepts
configurable columns it can no longer own its migrations, and owning the
migrations is what a consumer is actually adopting — the schema, the indexes
that make the reads fast, and the guarantee that both move together. An avatar
reference into another domain and a birthday for an age gate are the two
examples that motivated this package's existence, and both are exactly the kind
of thing a side table holds well.

# Getting the tables

The DDL lives in [github.com/primandproper/platform-go/v13/identity/migrations],
rendered per dialect and table prefix, and hands to database/migrate's
WithGeneratedMigration so nothing is copied into a consumer's repository. See
that package for why no numbered migration file ships.

That package also answers which tables exist, at your prefix, through its Tables
function — the list is complete and read from the DDL, so a between-tests
TRUNCATE, a backup policy or a privacy inventory names every one of them without
anybody copying seven names out of the schema.

# What a consumer still writes

The service layer, and that is the point of the split. Registration policy —
whether an invitation is required, what a username may look like, which
transactional email goes out — is application judgement. This package gives
that service a place to put the result, in one transaction:

	err := client.WithTransaction(ctx, func(q database.Tx) error {
		if err := store.CreateUser(ctx, q, user); err != nil {
			return err
		}
		if err := store.CreateAccount(ctx, q, account); err != nil {
			return err
		}
		return store.CreateMembership(ctx, q, membership)
	})

The three writes that make a registration are the three this package makes
transactional, because a user without an account, or an account without an
owner, is the failure mode every application discovers in production rather
than in a test.

# Where the SQL comes from

Every statement this package executes is generated, through a pipeline with two
committed artifacts, and the duplication between them is deliberate.

The schema's facts — the table names, each table's columns in projection order,
the subsets a write may assign — are spelled once, in identity/internal/queries.
`make generate` renders them through database/querygen into the canonical .sql
files beside that package, in sqlc's spelling: named statements whose arguments
are sqlc.arg references. sqlc compiles every one of those statements against the
DDL identity/migrations produces, on all three dialects, and sqlc-gen-unison
emits identity/internal/identitydb from them — typed params and methods over
driver placeholders — which is what the store executes. A column that does not
exist is a build failure with no database running, where it used to be a scan
error at runtime.

Committing the generated Go is not a choice: consumers compile this module from
the module cache, so the package the store executes has to be in the tree. The
.sql could in principle be rendered into a temp directory on each generation and
never committed, and it is committed anyway because of what would go with it.
It is the reviewable form of the contract — the generated Go embeds the same
statements, but in driver spelling, argument names erased into positional
markers, where the .sql's sqlc.arg(current_owner_user_id) is the spelling in
which a reviewer can see that a guard and its assignment are two arguments. It
anchors the drift gate — a test pins the committed text byte for byte against
the renderer, so "the SQL sqlc checks is the SQL the store runs" is a fact a
test states rather than a property of a pipeline taken on trust. And it is the
debugging seam when sqlc rejects a statement, because the file is exactly what
the analyzer was handed. The two cannot drift from each other: one is rendered
from code, the other is generated from the first, and the gates hold both ends.

The three reads that cross the membership junction come from the same place —
an account's roster, the accounts a user belongs to, and the user's own
membership list — because querygen renders a junction list as well as a
single-table one. The roster projects the member's columns beside the
membership's under a user_ prefix, so a page of thirty members is one query and
the row it comes back as is generated rather than paired to a Scan by eye. The
username prefix search is a rendered pair the same way: the page and the count
beside it, which is the one read here whose statement is not a filtered list.

The batched reads come from the same place, and they were the last reads that
did not. A page's rows point at users — "created by" — and a page of users,
memberships or invitations has roles hanging off it; read one key at a time
that is a round trip per row, and the loop converting rows is where that shape
arrives without anybody choosing it. Each is a read keyed on a bound set, which
querygen renders as a bound array on Postgres and a placeholder expansion on
the other two, under one Go signature. What each still owes its caller is the
empty batch, answered before the query rather than by it.

The two writes whose guards are not equalities came the same way once querygen
learned to say them: the two-factor verification, which stamps a proof only
where a secret exists and has not been proven, and the collision check behind
ErrUsernameTaken, which excludes the row being updated through an argument a
registration simply does not send. Both were hand-built for exactly as long as a
predicate meant "this column equals a bound value".

What remains hand-written is six builders and the runtime binder they render
through, and they are worth counting by shape rather than by name, because a
shape is what the port still owes a generator. One is a predicate querygen has
no form for: the default-flag clear, whose predicate excludes the membership
being set rather than matching one. One is an update whose SET list is chosen
per call, the agreements a registration accepts in a single statement. The last
four are membership statements addressed by the (user, account) pair — an
archival, a default set, a count of what is live — and the archive-by-side,
whose column is chosen per call because one statement ends a user's memberships
and an account's. The DELETEs used to be on this list and are not any more: the
erasure and both halves of a role-set rewrite are querygen.Generator.DeleteQuery
and InsertQuery statements now, one insert per role in place of the multi-row
VALUES list whose shape was the caller's cardinality and which sqlc therefore
could never check.
*/
package identity

//go:generate go run ./internal/queriesgen
