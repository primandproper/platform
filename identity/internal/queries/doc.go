/*
Package queries is the identity schema described as data: the canonical table
names, each table's columns in the order every read projects them, and the two
subsets a write may assign.

It exists because those facts now have two consumers that must not disagree.
The identity store renders them through database/querygen's Bound methods, with
the consumer's table prefix on the name, and executes what comes back; the
generator behind `make generate` renders the same tables through
[querygen.Generator.StandardCRUD] into the canonical .sql files sqlc is run
over. A column list spelled in both places could differ in one name, and the
symptom would be a check that passes over SQL nobody executes.

So it is spelled once, here, and both halves read it. The .sql files beside this
file are the generator's output — see [Render] and identity/internal/queriesgen.
Why the rendered .sql is committed at all, when the generated Go beside it in
identity/internal/identitydb carries the same statements in executable form, is
identity's package comment, under "Where the SQL comes from".

# Which queries each table gets

[Table.Options] is where a table says what it is beyond a list of columns, and
three of the four options carry a fact that a column list cannot:

  - Ownership is the scope column, so every emitted statement is keyed on it.
    It is named rather than inferred, because a table whose rows are readable
    across scopes and one whose rows are not look identical from the columns.
  - Nullable names the columns a write may set to NULL, which lives in the
    schema neither this package nor querygen reads.
  - Updatable names the columns the standard update assigns, and everything
    else assignable becomes immutable to it. It is stated positively because
    that is the shorter and the more checkable half: a user has four profile
    columns and ten written only by the method that owns them, and a list of
    the ten is a list somebody adds a column to a table without extending.
    Getting it wrong is not a small thing — querygen assigns every column its
    options leave mutable, and the struct a caller is holding is often a
    [identity.User.Redacted] copy whose credential fields are empty, so a
    password hash left in the update set is blanked on every profile save.

The columns Updatable leaves out are not columns nothing writes — they are the
columns a *named* statement writes, and those statements are emitted too. See
fieldWrites: the password and its stamp, the two-factor secret and its
verification, the email verification token, the account status, the ownership
transfer, the invitation answer. Each names its own SET list rather than the
table's mutable set, and three of them carry a predicate on the value being
replaced, which is what makes a losing concurrent writer report zero rows
instead of overwriting the winner.

Memberships is the fourth table and gets none of the standard set. Its columns
are textbook and not one of its statements is: the get, the archive and the bulk
archive key on the (belongs_to_user, belongs_to_account) pair rather than on id,
which the standard set has no way to express.

Its write is emitted, though — [querygen.Generator.UpsertQuery] renders it — and
it is the statement whose three dialect files genuinely diverge rather than
merely renumbering their placeholders. A membership has to be written by
converging on the pair: it is unique across live and archived rows alike, so
rejoining an account revives the row that is already there rather than adding a
second, and the id it keeps is what the membership's roles hang off.

Its reads are another matter, and they are the three junction lists [Render]
appends after the standard sets. An account's roster is a page of memberships
with the member's columns projected beside them; a user's account list is a page
of accounts reached through the same table; a user's own membership list is the
unpaged read behind every authorization decision this package answers. All three
were hand-built until querygen learned the shape, and the roster was what kept a
two-entity scan-by-position pairing alive after everything single-table had been
generated.

# The keyed variants

A table's standard queries are not all of what a store runs against it, and the
difference used to be the hand-written half. A read keyed on a reference, a read
of one database-owned column, a read keyed on a natural key the table carries an
id alongside — each of those was rendered by the store and by nothing else, so
sqlc proved statements the store did not run while the store ran statements sqlc
never saw.

So they are rendered here too, through the Query forms beside querygen's Bound
methods, which are the same statement text by construction:

  - the two paged invitation reads, keyed on the sender or the addressee
  - the read-back of created_at, one per emitted table
  - the three single-user reads keyed on a username, an email address, or a
    verification token
  - the three membership reads, all keyed on the (user, account) pair

The membership ones are why Memberships is declared at all despite emitting no
standard query, and why [Table.KeyedColumns] exists: querygen derives a
statement's id predicate from the column list it is handed, so a read that keys
on the natural key hands over the list without the id while still projecting it.
*/
package queries
