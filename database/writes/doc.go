/*
Package writes owns the transaction a repository write runs in, and runs the
application's hooks inside it.

Every write method in a repository built on generated queries wraps its one
generated call in the same twenty-five lines: begin a transaction, run the
statement, check that it matched a row, append an audit entry, enqueue a data
change event, commit. In the application this was extracted from, that shape
appears at 83 sites across 30 repository files in 17 domains. What varies
between the copies is which fields the audit entry names and which event type is
emitted — parameters, not structure.

	if err := w.Do(ctx, func(ctx context.Context, exec database.SQLQueryExecutor) ([]writes.Change, error) {
		affected, err := q.generated.ArchiveServiceSetting(ctx, exec, id)
		if err != nil {
			return nil, op.Error(err, "archiving service setting")
		}

		if err = writes.RequireAffected(affected); err != nil {
			return nil, err
		}

		return []writes.Change{{
			Resource: "service_setting",
			Table:    "service_settings",
			ID:       id,
			Op:       writes.OpArchived,
			Scope:    scope,
		}}, nil
	}); err != nil {
		return err
	}

# Why this is extracted and the queries are not

The module's rule is to extract what can be got *wrong* twice, not what is
merely written twice. The queries are the second case: sqlc and a query
generator already own them, and a domain's SELECT differing from its neighbour's
is the point of having two domains. This is the first case. A write whose audit
entry is appended after its transaction commits, or whose event is emitted
outside it, is durable state diverging from the record that describes it — and
at the call site it looks identical to a correct one. Eighty-three hand-written
copies of "and these go in the same transaction" is a rule applied eighty-three
times and therefore applied inconsistently.

So the executor is the whole point of Hook. A hook takes the transaction its
write is running under, which makes what it writes further statements in that
transaction: they land with the row or they do not land. The cost is on the same
ledger and is stated plainly on Hook — a hook that fails fails the write.

# The write returns its changes

Do hands the write an executor and takes back what it did, rather than taking a
description of what it is about to do. The identity a hook needs is frequently
only known once the statement has run: an id minted during a create, the owner
read off a row immediately before it is archived, one Change per row for a
cascade over a set whose size nobody knew in advance.

# Change carries no row, so nothing here is generic

Change names identity — resource, table, id, owner, operation, scope — and not
the row those columns came from. The audit entries this was extracted from
record exactly that; the minority that also carry a field-level diff compute it
from a before-image the caller is already holding, so a hook that wants the row
closes over it at the one place it exists. Nothing here needs a type parameter,
which means nothing downstream has to spell one.

# What is deliberately absent

No declaration format, no column list, no accessors: the columns are in the
migration and in the query generator, and a third statement of them is a third
thing to keep in agreement. No SQL, rendered at runtime or otherwise — the
caller hands over a closure that calls its own generated querier. And no read
path, because the ceremony this exists to delete is on the write side.

There is also no audit hook shipped beside this. An adapter written against this
module's own audit.Recorder did not fit the application it was built for, whose
recorder takes its own entry type; a hook is six lines of the consumer's own
code, and the seam it plugs into is the exported Hook type. See #285.

# Where it lives, and why here

It takes a database.Client and hands out a database.SQLQueryExecutor, so it sits
under database/ with the types it is written in terms of. It is not a driver
concern and adds no dialect of its own: every statement it runs is the caller's.

# What it observes

Do begins a span and records the module's usual trio — attempts, failures,
latency — under a "database_writes" prefix. Failures are labeled with the stage
that produced them, because "the domain's statement failed" and "the audit hook
failed" are different alerts: the first is a bug in one query, the second is a
write path that is about to stop accepting writes.

It deliberately does not fold in the caller's own framing. The span a repository
method wants is named for that method, and the error it wants logged is
described in that method's words; Do is called from inside it and would name
both after itself. Keep observability.Observer where it already is and let Do
nest under it.
*/
package writes
