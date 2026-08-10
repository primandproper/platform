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

The database/sql layer carries the otelsql instrumentation (spans and the
db.sql.* metric series, unchanged from earlier releases); queries issued
natively through PgxAccess pools are not yet traced.
*/
package postgres
