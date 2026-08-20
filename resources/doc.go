/*
Package resources is a scoped-CRUD runtime: a resource declares its table, its
columns, and who owns its rows, and gets Exists/Get/GetMany/List/Create/Update/
Archive over that declaration without writing a store.

# What this replaces

An application whose domains follow one row convention — a sortable text id, a
created_at, a nullable last_updated_at, a soft-delete archived_at, and an owner
column — writes the same store per domain: the same six statements, the same
scan, the same cursor pagination, the same audit and event writes inside the
same transaction. The statements differ only in a table name and a column list,
and both of those are already declared somewhere: in the migration, and in
whatever drives the query generator.

So the declaration is not the thing being repeated. What is repeated is
everything derived from it, and this package is where that derivation happens
once, at runtime, instead of once per domain at generate time.

# What a declaration is

A Definition is an ordered column list and a handful of roles picked out of it
by name. Each column carries a name and an accessor — a function returning a
pointer to the field it maps to — and that one accessor serves all three
directions: dereferenced for a write, handed to Scan for a read, and read for a
hook. There is no struct tag and no reflection over the row type, because a
column whose field was renamed should stop compiling rather than start returning
zero values.

Nullability is not declared. It is read off the accessor's type: a field reached
as **string is a column that may be NULL, and one reached as *string is not.
That is the same fact the schema states, and stating it twice is how the two come
to disagree.

# Scope and owner are two different things

A row belongs to a tenant and a row has an author, and an application that
stores both in one belongs_to_user column has conflated a read filter with a
write gate. This package keeps them apart:

  - Scope is the tenancy dimension, a tenancy.Scope, and it filters every read.
    Every method takes one. A resource whose table has no scope column declares
    Unscoped, and its methods still take a scope and still refuse one that names
    a tenant — so adding the column later is a change to the declaration and not
    to any call site.

  - Owner is the row's author, and by default it gates writes only: anyone who
    can see a comment can read it, and only its author may edit it. A resource
    where the owner is also the only reader says OwnerReadsAndWrites.

Both predicates are the actor's, and the actor may be nobody: resources.System
is the write with no user behind it — a cascade, a retention reaper, a scheduled
finalizer — and its statements are keyed on the scope and nothing else. That is
a real widening of what a call can touch, which is why it is a named constructor
rather than something an empty id falls into, and why the statements it issues
are rendered separately rather than by binding an owner the system does not
have.

The distinction is what lets comments be declared honestly. Its belongs_to_user
is authorship — the reference read returns every author's comments on a recipe —
and modeling it as a tenancy scope would have produced a store whose list method
could not answer the question the application actually asks.

# Where the SQL comes from

Nothing in this package renders a predicate. The statements come from
database/querygen, which is also what the sqlc-generating side of an application
uses, so a resource served at runtime by this package and a table whose queries
are generated ahead of time filter identically — the archived toggle admits
rows the same way, the cursor predicate is absent from the counts the same way.
That is deliberate: those semantics can be got wrong twice, and a second
rendering of them here is exactly the drift the arrangement is meant to prevent.

Define also registers the table with querygen's registry, because a table served
here emits no generated queries and a consumer deriving its table list from what
its generator emitted would otherwise lose it. See querygen.Registry.

# Reads are keyed, not open

List takes matches, and a match set that does not correspond to a declared
Lookup is refused. A generic list method whose predicates are whatever the
caller passed is a sequential scan generator; requiring the combination to have
been declared means every read this package can issue is one someone chose to
index.

A match's value is an any, which is the one place a type this package could have
checked at compile time it checks at run time instead. Making it typed means
Column[T, V], and a column carrying its value type as a parameter cannot sit in
the []Column[T] a declaration is — the type argument would have to be written at
every column of every resource to buy a check on the handful of values a keyed
read binds. So the value is checked against the column's field type on the way
in, by kind rather than by identity, and a match that could only have failed at
the driver fails at the call instead. See Column.accepts and
ErrMatchTypeMismatch.

# What it deliberately does not do

It does not own the transport, the request types, or their validation. Create
takes the row, not a creation-input struct, because the conversion from a
request into a row is where an application's own opinions live and this package
has none. It does not publish events or write audit entries either — it calls
Hooks inside the transaction that did the write, and what those hooks do is the
application's business. resources/audithook is one such hook, for this module's
audit log, and it is a separate package for the same reason: a resource that
audits nothing should not import an audit recorder.

# Depend on your own interface, not on this one

Store is concrete, and there is no Store interface here on purpose.

A service that reads and writes comments needs four or five methods, and the
Go-idiomatic way for it to say so is a four-or-five-method interface declared on
the consumer's side:

	type commentStore interface {
		Get(ctx context.Context, scope tenancy.Scope, actor resources.Actor, id string) (*Comment, error)
		List(ctx context.Context, scope tenancy.Scope, actor resources.Actor, filter *filtering.QueryFilter, matches ...resources.Match) (*filtering.QueryFilteredResult[Comment], error)
		Create(ctx context.Context, scope tenancy.Scope, actor resources.Actor, row *Comment) (*Comment, error)
	}

That interface is six lines, its fake is six methods, and it names exactly what
the service is allowed to do. An interface exported from here would instead be
the union of every method every consumer might want: a service that only reads
would compile against a value that can also archive, and every test double
anybody wrote would grow a method the day this package did. The narrow interface
belongs to the consumer, so this package ships the implementation and no seam.

# Observability

A Store takes a logger, a tracer provider, and a metrics provider, each of which
is absent by default and records nowhere when it is. The metrics are the trio
every component in this module records — attempts, failures, latency — under one
"resources" prefix with the resource and the method as attributes, so one panel
covers every domain an application declares rather than one per domain that
somebody has to remember to add.
*/
package resources
