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

Memberships is the fourth table and gets no standard set. Its columns are
textbook and most of its statements are not: the get, the archive and the bulk
archive key on the (belongs_to_user, belongs_to_account) pair rather than on id,
and its write is an upsert that revives an archived row.

Its reads are another matter, and they are the three junction lists [Render]
appends after the standard sets. An account's roster is a page of memberships
with the member's columns projected beside them; a user's account list is a page
of accounts reached through the same table; a user's own membership list is the
unpaged read behind every authorization decision this package answers. All three
were hand-built until querygen learned the shape, and the roster was what kept a
two-entity scan-by-position pairing alive after everything single-table had been
generated.
*/
package queries
