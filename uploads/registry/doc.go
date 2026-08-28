/*
Package registry records what was uploaded: one row per object in storage,
saying who put it there, into which tenant, what it is, how big it is, and what
it belongs to.

uploads moves bytes to object storage and hands back a key. Nothing about that
key says whose it is, so every consumer that accepts uploads has written this
table itself — and the hand-written one is where a tenant column gets forgotten.

# Why the row is the access control

"May this caller read this object" is answered from metadata — the owner and the
scope on the row — not from the bucket. A consumer without the registry either
serves objects public-by-key, which makes an unguessable key the only protection
a private document has, or reinvents the registry. The row is also what makes
orphan detection possible at all: a row with no object, an object with no row.
Sweeping for either is deliberately not here yet; the registry existing is what
makes it writable later.

# It references the key, it never wraps the byte path

Nothing in this package opens, reads, or removes an object. Storing and
registering are separately callable on purpose — bytes that arrived through a
signed URL were written by the client, and a consumer adopting this against an
existing bucket registers objects nothing here ever wrote. [StoreAndRecord] is
the convenience that does both, in the order that fails safe, and it is a free
function over the two seams rather than a method on either.

Archival follows from the same split. [Store.ArchiveObject] is metadata-only: the
row is hidden and the object stays in the bucket, because whether a receipt is
still needed for tax purposes is the consumer's retention policy rather than
this package's guess. See the retention package.

# Tenancy

Every read is scoped and there is no unscoped variant of any of them. The scope
is a column — TEXT NOT NULL with deliberately no DEFAULT, since the empty string
is tenancy.Global() rather than "unset" — it is bound as a tenancy.Scope rather
than a string derived from one, and no read path omits it. An application with a
single tenant passes tenancy.Global() everywhere and behaves exactly as it would
have without the column.

# Usage

	store, err := registry.NewSQLStore(client)
	// ...

	object := &registry.Object{
		Scope:     scope,
		Key:       "avatars/" + userID + "/original.png",
		OwnerID:   userID,
		BelongsTo: registry.Subject{Type: "user", ID: userID},
	}

	if err = registry.StoreAndRecord(ctx, manager, store, object, upload); err != nil {
		// ...
	}

	// Later, deciding whether this caller may have the bytes:
	object, err = store.GetObjectByKey(ctx, scope, key)
	switch {
	case errors.Is(err, registry.ErrObjectNotFound):
		// no row: not this tenant's object, or not registered at all
	case err != nil:
		// ...
	case object.OwnerID != callerID:
		// a row, and it is not theirs
	}

# Where the SQL comes from

Every statement this package executes is generated. The corpus is rendered by
database/querygen from the column list in uploads/registry/internal/queries,
checked by sqlc against the DDL uploads/registry/migrations renders, and turned
into the executable querier by sqlc-gen-unison — one shared set of Go types over
three per-dialect statement sets. There is no hand-written SQL here and no
statement assembled at runtime.

The schema is the consumer's to migrate. uploads/registry/migrations renders the
DDL for a dialect and a table prefix; it ships no numbered migration file,
because migration numbers are global per consumer.
*/
package registry

// Regenerate the .sql corpus after changing uploads/registry/internal/queries or
// the DDL, then run `make unison` to regenerate the querier from it.

//go:generate go run ./internal/queriesgen
