/*
Package querygen emits sqlc input for tables shaped the way this module's row
conventions expect, in the dialect of whichever of the three databases this
module supports will run it.

The conventions are already load-bearing elsewhere. filtering.QueryFilter is a
window over created_at and last_updated_at, a cursor compared against id, and a
flag deciding whether archived_at rows count. search/sync's Scanner wants a
strictly ordered page of IDs and nothing else. database's soft delete is
archived_at rather than DELETE. None of that was ever written down as SQL, so
each consumer wrote the SQL themselves, once per table, and the conventions held
exactly as long as everyone remembered them — which is to say they held until
the first table where someone did not.

This package writes that SQL. Its first consumer is sqlc: the whole reason to
hand queries to a generator is that they are checked against the schema at build
time and come back as typed Go, so what comes out of [Generator.StandardCRUD]
and the fragment methods is text, to be written to a .sql file and fed to sqlc
alongside the schema. Nothing here assembles a query per request, and none of it
reads a schema.

There is one consumer, and it is that pipeline: render the corpus, check it with
sqlc, execute the querier sqlc-gen-unison generates from it. Nothing here renders
a statement for a driver, and a store executing SQL this package never emitted is
a store outside the guarantee — see "Porting a store onto this package" below.

# What a caller supplies

A dialect, through [For], which returns the [Generator] every emitter hangs off:

	queries := querygen.For(dialect.Postgres).StandardCRUD("widgets", columns)

Then a table name and its column list, in the order the emitted SELECTs should
list them. Everything else is read off the column set:

	created_at present      → the created_after/created_before window
	last_updated_at present → the updated_after/updated_before window
	archived_at present     → soft delete, and the include_archived toggle
	last_indexed_at present → the reindex scan search/sync reads through, and
	                          the bulk stamp that maintains it
	id                      → required by StandardCRUD; the cursor its list
	                          pages by, and every query's key

A query whose column is absent is not emitted, and a predicate whose column is
absent is not rendered. That is the point of deriving them: a table without
last_updated_at cannot end up with an Update that sets it, and a table with
archived_at cannot end up without an Archive.

last_indexed_at is the one that took two rounds to get right. Its presence has
always decided the reindex scan, and the column has always been database-owned —
excluded from the create and the update, so no caller can supply it. What was
missing was anything that wrote it: the scan walked a column the convention
forbade everyone from maintaining. MarkXAsIndexed is that write, emitted from
the same column list as the scan, and a searchsync.Syncer flushes ids into it
through searchsync.NewStampBuffer. The column, the query that reads it, and the
write that maintains it are one feature rather than three-quarters of one.

WithOmitted subtracts from that set, for a table whose rows are not addressable
the way it assumes — a child row written with its parent and never read on its
own. It cannot add: what comes out stays a subset of what the columns justify, so
the properties above survive a caller who reaches for it.

Two things a column list cannot say are said with options rather than guessed at.
WithNullable names the columns a write may set to NULL, which lives in the schema
this package never reads; WithDatabaseOwned and WithImmutable name the columns a
caller may not assign, which lives in the application. Guessing either produces
SQL that generates, compiles, and is wrong at runtime.

# Argument names

The emitted SQL binds sqlc arguments whose names are neither the Go field names
nor the query-parameter names. All three spellings exist and none of them can be
guessed from another, so they are written down here:

	filtering.QueryFilter    URL parameter      sqlc argument
	CreatedAfter             createdAfter       created_after
	CreatedBefore            createdBefore      created_before
	UpdatedAfter             updatedAfter       updated_after
	UpdatedBefore            updatedBefore      updated_before
	IncludeArchived          includeArchived    include_archived
	Cursor                   cursor             page_cursor
	MaxResponseSize          limit              result_limit

Two statements bind an argument that is not a filter field at all: ids, a whole
set bound at once — the rows the bulk stamp marks as indexed, and the keys a
batched read answers for. A [SetKey] can name it something else where the set is
not of ids.

One filter field binds nothing, and its absence from that table is the point.
SortBy names a direction, and a direction is which way the ORDER BY runs and
which way the cursor comparison points — statement text on all three servers,
with no expression that takes a bound value and orders by it. So a paged list is
emitted twice, under a name and [DescendingName] of it, and what a store does
with SortBy is choose between them. See [Direction] and
filtering.QueryFilter.SortsDescending, which is the one reading of that field
there is.

The keyset position is page_cursor rather than cursor because CURSOR is a
reserved word in MySQL — see filtering's own constants for why the name moved
rather than the dialect being special-cased. MySQL is also the one dialect whose
page size is bound through no name at all: its grammar takes a bare placeholder
after LIMIT, so the emitted SQL spells the marker directly and the generated
parameter is still named for result_limit — see identity's unison.yaml, where
the name converges.

# The keyed variants

[Generator.StandardCRUD] emits the set a conventional table gets: keyed on the
row's own id and, where the caller named one, on an ownership column. A store's
corpus is that set plus the statements its own reads need — a get keyed on a
natural key, a list keyed on a reference, an update guarded by the value it is
replacing, a read that projects one column.

[Generator.InsertQuery], [Generator.GetQuery], [Generator.ReadQuery],
[Generator.ExistsQuery], [Generator.ListQueries], [Generator.UpdateQuery] and
[Generator.ArchiveQuery] render those, each named and annotated for a query
file:

	list := querygen.For(dialect.Postgres).ListQueries(
		"ListInvitationsByFromUser", "identity_invitations", columns,
		querygen.Match{Column: "scope"},
		querygen.Match{Column: "from_user"})

Each calls the statement function StandardCRUD calls, with the matches where
WithOwnership's column goes, so a variant is the standard statement with more
predicates rather than a second rendering of one. The filter window, the archived
toggle, the cursor and the two counts are the same code path: a keyed read
filters exactly as an unkeyed one does because there is nothing that could make
it not.

The list is the one of them that is plural, because a paged list is two
statements: ListInvitationsByFromUser and ListInvitationsByFromUserDescending,
identical but for the cursor comparison and the ORDER BY. Emitting the pair from
one call is what keeps a corpus from carrying only the direction somebody
happened to think of — a store answering sortBy=desc with an ascending page is
not a failure any test of the ascending statement can see.

[Match] is the predicate — a tenancy scope, an owner, the reference a child row
hangs off — and it is a column name rather than finished SQL, because the
statements it lands in render it more than once. A list carries its predicates in
the SELECT and again in each of the two count subqueries beside it; a caller
handing over finished SQL would have to know how many times its argument was
about to appear. A [Match] can also exclude rather than include, for the read
looking for another row like this one.

[Generator.ReadQuery] is the one the standard get cannot express: a [Read] says
what the SELECT lists and — where the key admits more than one row — the column
whose order decides which one answers. The column list stays the table's shape,
which is what the id and archived predicates are derived from, so a table
carrying an id it does not key on leaves the column out of that list and names it
in [Read.Projection].

# Guarded writes

The update is not only the conventional whole-row one. It assigns the columns it
is handed, so a store's field-specific writes — the password and the stamp that
goes with it, a status and its explanation, a verification token — are that same
statement with a shorter SET list, and last_updated_at stamps by convention in
every one of them.

What turns a field-specific write into a safe one is a predicate naming the
value the row must still hold:

	update := querygen.For(dialect.Postgres).UpdateQuery("TransferAccount",
		"accounts", columns, []string{"owner_user_id"}, nil,
		querygen.Match{Column: "scope"},
		querygen.Match{Column: "owner_user_id", Arg: "current_owner_user_id"})

Two concurrent transfers there cannot both succeed: the second finds the owner
already moved, matches nothing, and its row count says so. That is the whole
mechanism, and it needs the guard and the assignment to be two arguments —
[Match.Arg] is what separates them, since both halves are the same column and
one name would set it to the value it was requiring it to already hold. The
statement is annotated :execrows for that reason: the count is the answer.

# What a predicate compares against

Not every guard is an equality against a value the caller has. A write that must
happen exactly once guards on the stamp recording that it already did, and a
caller has no value to bind for "has not happened yet"; a token is spent while it
is still live, and the value that decides is the server's clock. [Match.Against]
names what the column is compared against, and the closed set of answers is
[Comparand]:

	verify := querygen.For(dialect.Postgres).UpdateQuery(
		"MarkUserTwoFactorSecretVerified", "users", columns,
		[]string{"two_factor_secret_verified_at"}, nullable,
		querygen.Match{Column: "scope"},
		querygen.Match{Column: "two_factor_secret", Against: querygen.EmptyString, Exclude: true},
		querygen.Match{Column: "two_factor_secret_verified_at", Against: querygen.NoValue})

A secret that exists and has not been proven — and a replayed verification
matches nothing, writes nothing, and reports the zero rows its caller reads as
"not there" rather than moving the timestamp forward.

[Comparand] holds six members. [BoundArgument] is the zero value and the
equality every keyed read wants. [NoValue] is IS NULL, which is how this module
records that something has not happened yet — an unredeemed token, an unproven
secret, a key not yet shredded. [EmptyString] is the sentinel a TEXT NOT NULL
column holds when it holds nothing, so its excluded form is "this fact exists".
[CurrentTime] is the server's clock, which is the expiry sweep uninverted and the
still-live guard inverted. [BoundTime] is that same boundary read from a clock
the caller supplies. [OptionalArgument] is the equality a caller may leave unset.

The two temporal ones are alternatives rather than a preference, and which is
right is decided by where the column they compare was written from. CurrentTime
is for a deadline the server stamped, so one clock answers both halves.
BoundTime is for a deadline an application stamped from a clock it was handed —
sessions writes expires_at as now-plus-a-TTL from the clock its backend was
constructed with, and under test that clock only moves when a test moves it, so
a sweep asking the server for the time would be comparing two clocks years
apart. Binding the same clock's reading puts the comparison back inside one
clock, which is the property CurrentTime's doc asks for rather than an exception
to it.

[Match.Exclude] inverts all six rather than only the first, and every inversion
is a complement: IS NULL against IS NOT NULL, the empty-string equality against
the not-empty guard, "at or before the instant" against "after" it. So the sweep
that collects expired rows and the guard that refuses to spend them are one
Match with one bool between them, and there is no second spelling of the
boundary to disagree with the first — including between the two temporal
comparands, which render one operator pair between them.

Three of the six bind nothing at all, and that is what makes them guards rather
than predicates: the value compared against belongs to the statement, so there is
no argument a caller could leave unset to relax it. Naming a [Match.Arg] beside
one of them is [ErrArgumentlessMatch] rather than a field quietly ignored.

The presence-conditional predicate is the last of them, and it is one static
statement rather than SQL assembled per call:

	free := querygen.For(dialect.Postgres).ReadQuery(
		"GetUserIDByUsername", "users", nil,
		querygen.Read{Projection: []string{querygen.IDColumn}},
		querygen.Match{Column: "username"},
		querygen.Match{Column: "scope"},
		querygen.Match{
			Column:  querygen.IDColumn,
			Against: querygen.OptionalArgument,
			Arg:     "except_user_id",
			Exclude: true,
		})

That renders

	id <> COALESCE(sqlc.narg(except_user_id), '')

which excludes the row being updated when the caller names one and excludes an
id no row has when it does not — so the collision check a user's own profile
save runs and the one a registration runs are the same checked statement. It
rests on the same fact [Generator.CursorCondition] rests on: no id is empty.

# Reads that cross a junction

Everything above projects one table. The read that does not is the one a
many-to-many brings with it: an account's roster is the membership rows with the
member's own columns beside them, and a user's account list is the accounts
reached through those same memberships. Both were hand-written for as long as
this package was single-table, and the roster in particular kept a hand-paired
two-entity scanner alive — a projection in one file and a list of scan targets
in another, where a mismatch is a runtime scan error rather than a failed build.

[Generator.JunctionListQueries] renders them, and [Generator.JunctionListAllQuery]
renders the unpaged form. What a caller adds to a list is a [Junction]: the table
joined in, the two columns the join matches, whatever key the far side carries,
and — where the caller wants the joined row's columns too — the prefix they are
aliased under.

	roster := querygen.For(dialect.Postgres).JunctionListQueries(
		"ListAccountMembers", "memberships", membershipColumns,
		&querygen.Junction{
			Table:    "users",
			Column:   querygen.IDColumn,
			OnColumn: "belongs_to_user",
			Columns:  userColumns,
			Prefix:   "user",
		},
		querygen.Match{Column: querygen.BelongsToAccountColumn})

Three things about it are worth knowing before writing one.

The listed table is the one the page is a page of, and it is the caller's
decision rather than a property of the schema. The cursor walks its id and the
filter window bounds its timestamps, so a roster lists memberships and joins
users, while a user's account list lists accounts and joins the very same
memberships. Getting it the wrong way round produces a working query that pages
over an id the caller never sees.

The join contributes predicates rather than sharing the filter. The listed
table's archived_at is what include_archived admits rows through; the joined
table's is required to be NULL outright, because the filter window describes the
rows being listed and the joined row is a reference those rows hold. A roster
asked for archived memberships wants the memberships that ended, not the users
who were deleted.

And a projection spanning two tables is aliased or it is not projected. Two
tables following these conventions share most of their column names, so an
unaliased pair has two columns called id and two called created_at, and what a
generator downstream makes of that depends on the order the SELECT happened to
list its tables in. [Junction.Prefix] is what names them apart; leaving it empty
projects the listed table alone, which is what the accounts-through-memberships
direction wants.

# Tables with no id

[Generator.StandardCRUD] requires an id column and the keyed forms above do not,
and the asymmetry is the one place the two halves of this package genuinely
disagree about what a table has to look like.

StandardCRUD emits the list, and the list pages by keyset over the id: the cursor
predicate compares against that column, so it has to sort by creation time, and a
composite key is not a cursor without machinery this package does not have. The
single-row statements need no such thing. They need to address one row, and a
table whose primary key is (subject_type, subject_id) addresses one exactly by
naming both — which is what [Match] has always been for, an equality predicate on
a column, bound rather than interpolated. So the id predicate is rendered when
the column list has an id and not when it does not, exactly as the archived_at
predicate is, and [Generator.GetQuery], [Generator.ExistsQuery],
[Generator.UpdateQuery] and [Generator.ArchiveQuery] key a row on whatever it
actually keys on:

	get := querygen.For(dialect.Postgres).GetQuery("GetSubjectKey",
		"shredding_subject_keys", columns,
		querygen.Match{Column: "subject_type"},
		querygen.Match{Column: "subject_id"})

Four tables in this module are in that position, each with a natural key that
carries a meaning a surrogate id would not: audit_log_chains keys on its scope,
shredding_subject_keys on (subject_type, subject_id) — which is the constraint
enforcing one live key per subject, and so the difference between a shred that
works and one leaving half the ciphertext readable — metering_totals on
(subject, meter, period_start), and scheduled_timers on (timer_set, timer_key).

[Generator.InsertQuery] is here for the same table rather than for variety. An
INSERT keys on nothing, so it is the one statement such a table wants unchanged
from the standard set while wanting every other one keyed on its natural key —
and StandardCRUD, which would otherwise have emitted it, cannot serve the table
at all because of the list beside it. Without InsertQuery a natural-key corpus
would be five statements sqlc checks and a sixth nobody could render.

A statement that keys on nothing at all — no id in the column list and no [Match]
— is [ErrUnaddressableRow] rather than a statement whose WHERE clause is the
archived predicate alone. Reading one row by reading all of them is not a
degenerate read; it is a different query, and archiving through one empties a
table.

# The writes those tables are written with

A table with no id could be read and updated long before it could be created or
destroyed. [Generator.StandardCRUD] is where the create lived, and StandardCRUD
refuses a table with no id outright, so a child row keyed on its parent —
(membership_id, role), (user_id, role) — had no emitted insert at all; and
nothing here rendered a DELETE, so the hard deletes stayed hand-written in the
one place a consumer least wants hand-written SQL, the erasure a
right-to-be-forgotten request runs.

Three statements close that, and all three are corpus forms — named, annotated,
rendered into a .sql — with no [Bound] counterpart:

	roles := querygen.For(dialect.Postgres)

	insert := roles.InsertQuery("InsertMembershipRole", "membership_roles",
		[]string{"membership_id", "role"}, nil)

	clear := roles.DeleteQuery("DeleteMembershipRoles", "membership_roles",
		[]string{"membership_id", "role"},
		querygen.Match{Column: "membership_id"})

[Generator.InsertQuery] is the create with the id requirement lifted off it,
which is the only thing StandardCRUD's version had that an INSERT does not need
— the id is required there because the list pages by keyset over it, and an
insert has no list. A set of child rows is written one statement per element
rather than one statement with a VALUES list assembled per call: the multi-row
form's shape is the caller's cardinality, so it has no static text for sqlc to
check or for this package to emit, and the cardinalities are single-digit inside
a transaction the parent's write already opened.

[Generator.DeleteQuery] is the single-row machinery with a different verb. It
keys on the column list and the matches exactly as the get, the update and the
archive do, refuses [ErrUnaddressableRow] the same way, and is annotated
:execrows because the count is the answer. What it does not render is the
archived predicate, and that absence is the point: an erasure runs against a
subject who was archived first, and a role set is cleared whether or not its
parent has been, so a delete excluding archived rows would be the one write
unable to reach the rows it exists for. Its key need not name a single row —
clearing every grant a membership holds is one statement keyed on the membership
— which is the other half of what separates it from the archive.

[Generator.InsertIgnoreQuery] is the third shape, and it is not an upsert whose
conflict branch is empty: [ErrDegenerateUpsert] refuses that, correctly, because
an upsert that assigns nothing is an INSERT failing on its second call. This one
neither fails nor converges. The row already there wins, unchanged, and the
count says so — which is what a key mint wants, since the loser of a race
between two replicas has generated a key it must throw away. The key is the
conflict target under the upsert's rule, and the three dialects spell the shape
three ways: Postgres appends ON CONFLICT (…) DO NOTHING, MySQL and SQLite take a
modifier before INTO and name no target, so MySQL's skips a collision on any
unique key rather than on the one named — the same caveat the upsert carries.

# The prefix search

[Generator.PrefixSearchQueries] is the one read shape that is not a filtered
list: a page of rows whose column begins with what somebody typed, and the count
of everything that prefix matched.

	search := querygen.For(dialect.Postgres).PrefixSearchQueries("users", columns,
		querygen.PrefixSearch{
			Column:    "username",
			Name:      "SearchUsersByUsername",
			CountName: "CountSearchUsersByUsername",
		},
		querygen.Match{Column: "scope"})

Three things about it are not the standard list's, and each is why it is its own
shape rather than a [Match] on that one. One column is matched, ordered by, and
paged over, where a list orders by the id — a cursor names a position in an
order, so the search's is a keyset walk over the searched column. The count is a
second statement rather than a subquery riding on the rows, because the number a
caller wants is of everything the pattern matched rather than of what remains
after the cursor. And archived rows are excluded outright rather than through
include_archived: a name search is a lookup somebody is about to act on.

The page comes in both directions like every other paged read here, and what a
direction means is this statement's order rather than creation order: the
descending half walks the searched column backwards. That is the only reading
available to a statement that never orders by the id, and it is the one that
keeps the cursor and the ORDER BY naming the same order. The count is emitted
once, since a count does not depend on the order its rows would have arrived in.

The pattern is an argument rather than something the SQL assembles, and
[PrefixPattern] is what builds it — the wildcards escaped, a trailing % added,
and [LikeEscape] as the escape character the emitted ESCAPE clause names. Both
halves are here because they are one decision: a caller that binds a raw prefix
leaves whatever wildcard somebody typed a wildcard, so a prefix of "%" returns
every row — which reads as a working search returning too much rather than as a
bug.

# The batched read

Every N+1 read has the same shape underneath: a page of rows, and a second table
holding what hangs off each of them. Read one key at a time it is thirty round
trips returning two rows each — a roster page whose members' roles are fetched
inside the loop that converts rows. [Generator.SetReadQuery] is that read done
once:

	roles := querygen.For(dialect.Postgres).SetReadQuery(
		"ListMembershipRolesByMembershipIDs", "identity_membership_roles",
		[]string{"membership_id", "role"},
		querygen.Read{Order: "role"},
		querygen.SetKey{Column: "membership_id"})

The set is one argument on Postgres, which has arrays, and a sqlc.slice
expansion on the other two, which do not — the same divergence the bulk stamp
carries, and the same []string on either side of it. What the caller writes does
not move.

Three things about it are the statement's rather than a caller's.

The ordering is the keyed column's, so a consumer walks the rows once and sees
one key's rows together. [Read.Order] is the tie-break inside a group rather
than the order of the page.

The set is rendered after every [Match], and that is a requirement rather than a
layout choice: an expanded set is a run of bare markers, SQLite numbers a bare
marker one past the highest it has seen, and an argument bound after one
collides with an element of the set — matching nothing, quietly. Rendering it
last is what keeps one argument order right on all three engines.

And the empty batch is the caller's to answer before the query runs. There is no
text to emit for a zero-length set — `IN ()` is a syntax error on two dialects —
so what an empty slice does is a convention of whatever generated the Go, and
the conventional answer is a NULL that matches no row. That is a round trip
whose answer was known before it was sent, on a path whose whole purpose is
saving round trips. Nothing here can enforce the contract, because the arity is
the caller's and this package emits text.

Whether archived rows come back is decided the way every other predicate here is
decided — by the column list. A read whose columns carry archived_at excludes
them; a hydration read naming rows that other rows already point at hands over a
column list without it, and keeps them, because hiding a soft-deleted user turns
"created by a departed colleague" into "created by nobody".

It is corpus-only like everything else here — rendered into a consumer's .sql,
checked by sqlc, and executed through the method sqlc generates — but for this
shape that was never going to be a choice: a set reference has no fixed number
of markers, because sqlc expands it per call, so the statement's arity belongs
to the values and only the generated method can hold it.

# The table registry

Some of what a consumer needs per table is not a query. The TRUNCATE an
integration suite runs between tests is a list of table names; so is a schema
inventory, or a check that every table has a migration. The list has to be
complete, because the symptom of a missing entry is not a failure where the
mistake was made — a table left out of that TRUNCATE is a test somewhere else
failing later, on rows the previous test left behind.

The obvious place to get the list is wherever the per-table code lives, and that
is the trap. A generator with one builder per table doubles as a table list right
up until one table stops needing a builder — because its SQL now comes from
somewhere else, or because it never came from a generator at all — and then the
list is short by one with nothing to say so. The list survives only if it is fed
by the table existing rather than by something choosing to emit its queries.

So [Generator.StandardCRUD] registers every table it emits for, [RegisterTable]
takes the ones it does not, and [RegisteredTables] reads the union back:

	querygen.RegisterTable("sessions", "webauthn_credentials")

	tables := querygen.RegisteredTables()

Two sources, one list. A consumer reading that list does not have to know which
tables came from where, and a table moving from one source to the other does not
change what comes out.

The convention for a package in this module that ships a schema, which identity
is the worked example of: its generator registers every table it owns — the whole
list, not the subset [Generator.StandardCRUD] happens to emit for — and its
migrations subpackage exports a Tables(prefix) beside SQL and Statements for the
consumer half. The two halves answer the same question for different callers.
This one is for a generator binary reading back what it generated across
schemas, at the canonical unprefixed names; Tables is for the consumer, at
theirs, and reads the DDL, so neither depends on the other staying in step by
anybody's memory.

# Porting a store onto this package

There is one runtime tier, and every store that owns SQL is on it or is on its
way onto it. A store does not render statements for a driver; it renders a
corpus, sqlc checks the corpus against the schema, and sqlc-gen-unison generates
the typed querier the store calls. A column renamed in a migration is then a
failed generate rather than a runtime error, on every table, in all three
dialects.

identity is the worked example, and the shape a new store copies is four pieces:

	<pkg>/internal/queries      the schema as data: table names, each table's
	                            columns in projection order, and the subsets a
	                            write may assign — spelled once, because the
	                            corpus and the store both read them
	<pkg>/internal/queriesgen   a main that renders that data through this
	                            package into one .sql per dialect, and prints
	                            the DDL sqlc reads them against
	<pkg>/unison.yaml           the dialect roster, the generated package's
	                            name, and the type overrides
	<pkg>/internal/<pkg>db      the generated querier, committed

The rendered .sql is committed and nothing imports it: it exists so `sqlc
compile` can check it with no database running, and so the generated-files job
can diff it. `make generate` writes it, through a go:generate line on the
package; `make unison` renders the per-dialect schema beside it and runs the
emitter over the pair. Its script names the components it walks, and a new store
is a line in that list.

What a store writes by hand is which statements it wants, in the internal
queries package: [Generator.StandardCRUD] for a table whose reads are the
conventional set, and the keyed forms above for everything else. What it does not
write is SQL.

# The packages that are not on this tier

The registry's problem has a larger version one level up. One tier executes this
module's SQL: a package renders its statements into a canonical .sql, sqlc checks
them against that package's own schema on each dialect it serves, and the store
executes the querier the generator emits. Every package here that owns tables is
on that tier or is being ported onto it — and "every package" is a claim about a
set, which is checkable only if the exceptions to it are named. An exemption nobody wrote
down is indistinguishable from a package somebody missed: both look like a
package that simply never comes up. The reader who notices reconstructs the list
by grepping the module for SELECT, which is a survey with a shelf life of one
branch, and the survey that produced this section had to do exactly that.

So the boundary is stated here, and internal/sqltier is where a build checks it
is still where this says: it walks the module for the packages holding SQL and
fails on one no ruling covers, in either direction — a package that grew a
statement and a ruling that outlived one.

Four packages hold SQL that is not table SQL, and a corpus has nothing to say
about any of it:

  - database/postgres/tableaccess and database/mysql/tableaccess create users,
    grant privileges, and read the server's own catalogs. sqlc generates queries
    against a schema; it has no spelling for CREATE USER or GRANT, and pg_roles
    is not in any schema this module ships. Each is single-dialect because its
    statements are, rather than because nobody reached the other two.

  - distributedlock/postgres calls pg_try_advisory_lock and its siblings. The
    lock is a number the server holds for a session: no table, no schema, and no
    projection for a generator to type.

  - database/migrate asks a connection which schema its search path resolves to,
    so migrations of one schema serialize against each other rather than against
    the whole server. goose owns the bookkeeping table and ships its DDL; this
    package owns one session-scoped question.

search/vector/pgvector is exempt for a different reason, and the reason is not
the one that looked likely. Its operators were worth checking rather than
assuming, and sqlc accepts them: `embedding <=> $1::vector` parses, generates,
and comes back typed as interface{} on both sides, because a vector is an
extension type the analyzer resolves nothing for — and a column type override
reaches the stored column while leaving the distance an ANN search exists to
return untyped. That alone would only weaken the guarantee. What removes it is
that the index table is a runtime product of configuration: its name, its
dimension, and its metadata column's name are all values a caller supplies, and
the manager issues the CREATE TABLE itself at startup. There is no committed DDL
for sqlc to check a statement against, and no fixed column name for a statement
to project.

filtering holds no SQL at all. It was surveyed at one keyword and the keyword is
a word in a comment: what the package supplies is the argument names a rendered
statement binds — the seven above — and the conversions that bind them, which is
why every statement here says created_after rather than inventing a spelling of
its own. That it holds none is recorded as an assertion rather than left as an
absence, because an absence goes on reading true after the package stops
deserving it.

The remaining two are ports rather than exemptions.

authorization/database owns four tables and builds thirteen statements over
them, which a survey counting functions that return a query and its arguments
read as zero: these return the query alone, and their arguments are assembled at
the call site. Roles and permissions carry the convention triple, so the reads
and the writes over them are this package's ordinary shapes, and the shapes the
survey found missing have since landed: the mapping rows between them are the
id-less child tables [Generator.DeleteQuery] and [Generator.InsertQuery] now
serve, and the seed's lookups over a bound set are [Generator.SetReadQuery]. What
the port still waits on is the resolution query, a recursive closure — a shape
this package cannot render at all. sqlc is not the obstacle; it analyzes that
query on all three engines, binding the role names through ANY on Postgres and
sqlc.slice on the other two.

dataprivacy/auditerasure owns no table. Its three statements — two deletes of a
subject's audit scopes and the count of what the hash chain will not let go of —
address the audit log's tables, which the audit package ships the migrations
for. So they belong in that package's corpus rather than in one of its own: a
second corpus over somebody else's schema would be a second place a column
rename has to be noticed. It ports when the audit log does — the delete shape it
was waiting on is [Generator.DeleteQuery] now.

# include_archived actually includes archived rows

A filtered list's WHERE clause is FilterConditions in its entirety, not an
addendum bolted onto a WHERE the caller opened with archived_at IS NULL. The
distinction is the difference between a working toggle and a decorative one: a
query reading

	WHERE t.archived_at IS NULL
	  AND (NOT COALESCE(sqlc.narg(include_archived), false) OR t.archived_at IS NULL)

parses, runs, reports no error, and returns the same rows for either value of the
flag, because the first predicate has already decided. Owning the whole clause is
what makes that unrepresentable.

# The three dialects

Postgres, MySQL and SQLite each get SQL their own server parses, and almost all
of the difference is a handful of expressions: the case-insensitive substring
match, the pattern a prefix search's LIKE binds, the byte-ordered comparison the
reindex scan walks, the sentinel an unset time bound coalesces to, the precision
the current time is stored at, the nullable boolean the archived toggle binds,
the page-size clause, and the set membership the bulk stamp and the batched read
key on. They live
together in generator.go, as
unexported methods, so that what this package assumes about a server is one
screen rather than a grep for casts. The statement shapes those land in, the
query names, and which queries a column list justifies are the same on all
three.

Two statements are the exception, and both are an INSERT that has to do
something about a row already there — which is the one thing the three engines
never agreed on. An upsert is two grammars rather than one grammar with a
substituted expression: Postgres and SQLite name the conflict target and read
the incoming row through the EXCLUDED alias, and MySQL names no target at all —
its ON DUPLICATE KEY UPDATE fires on whichever unique key was violated — and
spells the incoming value VALUES(column). The insert-ignore divides them
differently again: Postgres alone has no modifier for it and takes a trailing ON
CONFLICT … DO NOTHING, while MySQL and SQLite each spell it before INTO, as
INSERT IGNORE and INSERT OR IGNORE. Every half of both is in generator.go with
the rest, so the one-screen property survives; what a consumer sees is still one
query name with one signature, rendered per dialect by
[Generator.UpsertQuery] and [Generator.InsertIgnoreQuery].

The set is closed at the type. [For] takes a dialect.Dialect and rejects one
outside dialect.Valid rather than emitting a plausible default, and the dialect
binds to the [Generator] rather than to each call, so a Postgres fragment cannot
be spliced into a MySQL statement. That matters more than it sounds: the failures
are asymmetric. COLLATE "C" in MySQL is a parse error, which is the good case;
ILIKE has no SQLite spelling at all, and the substitute folds a narrower set of
characters, which is a search that quietly misses rows.

What a consumer sees is one set of sqlc methods with one set of signatures
whichever dialect generated them, so the application code above them is written
once. Two exceptions, both from sqlc's own inference rather than from anything
here: the archived toggle carries a ::boolean on Postgres and cannot elsewhere,
because MySQL and SQLite have no boolean type to cast to; and a bound set — the
bulk stamp's ids, a batched read's keys — is an array on Postgres and a
sqlc.slice expansion on the other two, which changes what reaches the server and
not the []string a caller passes.

# What each dialect asks of a schema

A table generated for SQLite has to store its timestamps the way SQLite's own
CURRENT_TIMESTAMP writes them — YYYY-MM-DD HH:MM:SS, UTC. SQLite has no date
type, so the filter window's comparisons are lexicographic over text, and text in
any other shape compares in an order that is not chronological. The other two
have real timestamp types and no such requirement.

A table generated for MySQL needs its id column to be something MySQL will index
as a key: TEXT cannot be a primary key there without a prefix length, so ids
belong in a VARCHAR. Nothing in this package enforces either of these; both are
schema decisions, and this package never reads the schema.

# The one place a dialect changes a signature

Everything above is a difference in SQL under a Go API that does not move. LIMIT
is the exception, and it is worth knowing about before choosing MySQL.

Postgres and SQLite take an expression after LIMIT, so an absent page size
coalesces to filtering.DefaultQueryFilterLimit and the generated parameter is a
pointer a caller may leave nil. MySQL takes an integer literal or a placeholder
and nothing else — COALESCE there is a parse error rather than a slower plan — so
its LIMIT binds the size and the generated parameter is a value. Leveling the
other two down to match would take a working default away from the dialects that
can express one in order to make a limitation uniform, which is the wrong way
round.

Nothing drifts by leaving them different: the default is filtering's constant
rather than a number written here, so the SQL and
filtering.QueryFilter.Normalize read the same one. What a MySQL consumer owes its
queries is that Normalize call — it turns an absent or zero page size into that
constant and clamps an oversized one, the same treatment the URL parameter gets.
A MySQL query handed a zero returns no rows, which is loud, rather than a page of
some other size.
*/
package querygen
