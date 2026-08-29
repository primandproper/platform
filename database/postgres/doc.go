/*
Package postgres provides an interface for writing to a Postgres instance.

The client is pgx-native-first: each side of the read/write split opens a
*pgxpool.Pool, and the database/sql surface is derived from that pool via a
pool connector, so both surfaces share one set of connections and one
configuration.

NewDatabaseClient returns this package's *Client, so a caller who has chosen
postgres reaches the concrete handles as plain methods: the *sql.DB pair, and
the native pools for driver features the database/sql surface cannot express
(CopyFrom bulk loads, pgx.Batch, native array binding, LISTEN/NOTIFY). A caller
holding the portable database.Client instead — because their wiring chose the
driver from config — reaches the same handles behind two opt-in capabilities,
obtained by type assertion: database.RawAccess for the *sql.DB, and this
package's PgxAccess for the native pools.

Both surfaces are traced. The database/sql layer carries the otelsql
instrumentation (spans and the db.sql.* metric series, unchanged from earlier
releases), and the pools carry a pgx tracer for statements issued natively
through PgxAccess — Query, QueryRow, Exec, SendBatch, CopyFrom, and Prepare —
spanned in otelsql's own sql.* naming so that a trace reads as one database
rather than two.

Each statement is spanned once. The two surfaces run on the same connections,
so pgx's tracer hook fires for the database/sql layer's statements as well; the
client marks the contexts belonging to that layer and the pgx tracer skips them,
leaving otelsql's span the only one. What arrives unmarked is exactly what came
in through a pool a caller took from PgxAccess.

Both instrumentations resolve the provider given to WithTracerProvider, and
neither is installed without one — a client built with no tracer provider traces
nowhere rather than falling back to OpenTelemetry's global provider.
*/
package postgres
