/*
Package settings stores the runtime settings a user or an account sets about
themselves: administrator-defined definitions with a kind, a default and an
enumeration, and per-subject values stored against them.

	definitions, err := store.CreateDefinition(ctx, tenancy.Global(), &settings.Definition{
		Name:        "notifications.digest",
		Kind:        settings.KindString,
		Default:     pointer.To("weekly"),
		Enumeration: []string{"daily", "never", "weekly"},
	})

	// On the request path.
	if _, err = store.SetValue(ctx, tenancy.Global(), me, "notifications.digest", "daily"); err != nil { ... }

	resolved, err := store.Resolve(ctx, tenancy.Global(), me, "notifications.digest")
	digest, err := resolved.String() // "daily", and "weekly" for anyone who has not chosen

# This is not featureflags, and it is not config

Three things in this module answer the question "what value should this code use
here", and they are not interchangeable. Getting them confused is the failure
mode this package has, so it is worth being explicit about which is which.

config is boot-time environment. It is read once, by the process, out of the
environment or a file; nothing writes it back, and every request in the process
sees the same answer. If the value changes when somebody redeploys, it is config.

featureflags is a vendor's evaluation. A flag is somebody's rollout decision —
percentage rollouts, targeting rules, a kill switch — evaluated per request
against an external service, owned by whoever is shipping the feature. It is not
storage: this module holds no flag values, and a flag that does not exist is a
distinct answer rather than a missing row. If the value changes when somebody
moves a slider in LaunchDarkly, it is a flag.

This package is neither. A setting is a fact a user or an account chose about
themselves — their notification digest, their time zone, whether they want the
compact layout — stored in the consumer's own database, readable and writable by
the person it is about, and durable across deployments and vendors alike. If the
value changes when somebody clicks "save" on their own preferences page, it is a
setting.

The temptation this package has to resist is growing into a second featureflags:
targeting rules, percentage rollouts, an evaluation order across subject types.
Every one of those is a decision somebody makes about a population, which is what
a flag is for, and none of them is a fact a subject chose about themselves. What
this package will grow is storage-shaped: more kinds, better reads, a bulk write.
A rule engine belongs on the other side of that line.

# The two halves, and the rules between them

A [Definition] is what a setting is: the name application code asks for, the
[Kind] its values parse as, the [Definition.Default] a subject who has not chosen
falls back to, and the [Definition.Enumeration] of values it admits. A [Value] is
one [Subject]'s answer.

Split like that, three integrity rules exist between the two halves, and they are
exactly the rules that drift when the pair is hand-rolled in an application:

  - A value with no definition. Every write here reads the definition first, in
    the same transaction, and the schema's foreign key holds the same line for a
    writer that did not come through this package.
  - A value outside its definition's enumeration. Checked at every write, against
    the definition read inside that write's transaction.
  - A definition change that strands stored values. Narrowing an enumeration or
    changing a kind decides how every value already written is read, so
    [SQLStore.UpdateDefinition] walks the live values first and refuses the edit
    at the first one the new definition would not admit — [ErrStrandedValues],
    naming the subject and the value. The alternative is not a smaller problem:
    it is rows that exist, resolve, and fail to parse, for the subjects who chose
    a value somebody has just made illegal.

# Resolution has three answers, not two

	(value, SourceSubject)  the subject chose it
	(value, SourceDefault)  they have not, and the definition has a default
	("",    SourceUnset)    they have not, and it does not

The third is why [Resolution] is a value rather than four getters on the store.
A typed getter taking a fallback cannot express it — it answers "nobody has said"
with whatever the caller guessed, and gives the caller no way to tell that is
what happened. So the tri-state is a [Source] on the resolution and an
[ErrSettingUnset] from the typed accessors, which a caller matches:

	switch retention, err := resolved.Int(); {
	case errors.Is(err, settings.ErrSettingUnset):
		// Nobody has decided. The caller's own policy applies.
	case err != nil:
		// A kind mismatch, or an unparseable row.
	default:
		// retention is somebody's decision.
	}

[Definition.Default] is a *string for the same reason. A text setting defaulting
to "" answers every subject who has not chosen; a text setting with no default
answers none of them, and a plain string column has nowhere to put the
difference.

# Joining a caller's transaction

Every write here has a form that runs inside a transaction the caller owns:
[Store.CreateDefinitionTx], [Store.UpdateDefinitionTx],
[Store.ArchiveDefinitionTx], [Store.SetValueTx] and [Store.ClearValueTx], plus
[Store.DeleteValuesForSubject], which has no other form. Each takes a
database.Tx rather than reaching for the store's own writer, and the type is
what says so — only database.RunInTransaction produces one, so the obligation
is the compiler's rather than a doc comment's.

The reason is that a setting is rarely the only row a consumer writes. An audit
entry naming who changed what and a data change event on an outbox somebody fans
out are the ordinary companions, and a companion written after the setting's own
write has committed is a companion that can go missing while the setting stays.
The gap is narrow and it is one-directional — a value with no event, never an
event naming a value that was not written — and nothing outside this package can
close it, which is why these are here rather than left to a consumer to work
around.

Every read the writes depend on runs on that executor too: the definition a
value is checked against, the name a definition is checked for, and the walk
[Store.UpdateDefinitionTx] runs over stored values. So a definition and a value
against it can be written in one transaction, and so can a clearance and the
narrowing that would otherwise have been refused on its account —
[Store.UpdateDefinitionTx] says what that changes.

# Erasure

Clearing is an archive, and an archived value still says what the subject chose.
That is what an audit of a preference change reads, and it is also what a
subject access request has to remove, so [Store.DeleteValuesForSubject] is the
one hard delete here: everything one subject answered within a scope, cleared
answers included, from the transaction that removes the rest of them. A
deployment whose values all belong to one subject type can cascade from that
table's delete instead. One with two cannot, because a mixed subject_id column
cannot reference two tables, and this delete is what its dataprivacy.Eraser is
built on.

# Scope

Every method takes a tenancy.Scope, and there is no unscoped read of anything
here. A deployment with one catalog of settings passes tenancy.Global()
everywhere and behaves exactly as it would have without the column.

A definition and the values stored against it share a scope. That is a real
restriction and it is deliberate: a global catalog with per-tenant values would
put two scopes into every resolution, and a read whose whole guarantee is that it
names one scope cannot name two without the guarantee becoming a convention. A
deployment whose tenants share a catalog and differ in their answers gives both
the tenant's scope and seeds the definitions per tenant, which is an
administrative write and a cheap one.

# Where the SQL comes from

Nothing in this package composes SQL. The statements are rendered from the column
lists in settings/internal/queries through database/querygen, committed as one
.sql per dialect, checked against the schema by sqlc, and executed through the
querier sqlc-gen-unison generates into settings/internal/settingsdb. A column
renamed in settings/migrations is a failed generate rather than a runtime scan
error, on every table, in all three dialects.

The tables are settings/migrations' to create, at whatever prefix a consumer
chooses. The platform ships no numbered migration file — see that package.

# Where this package stops

At the store. A settings API's routes, its request and response types, and who
is allowed to write a definition as opposed to read one are an application's,
and this package ships none of them. The module README's "Transports" section
is where that line is drawn for the module as a whole.
*/
package settings

//go:generate go run ./internal/queriesgen
