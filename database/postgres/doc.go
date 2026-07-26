/*
Package postgres provides an interface for writing to a Postgres instance.

The client is pgx-native-first: each side of the read/write split opens a
*pgxpool.Pool, and the database/sql surface is derived from that pool via a
pool connector, so both surfaces share one set of connections and one
configuration. Portable access goes through database.Client as usual; the
concrete handles are available behind two opt-in capabilities, obtained by
type assertion — database.RawAccess for the *sql.DB, and this package's
PgxAccess for the native pools, for driver features the database/sql surface
cannot express (CopyFrom bulk loads, pgx.Batch, native array binding,
LISTEN/NOTIFY).

The database/sql layer carries the otelsql instrumentation (spans and the
db.sql.* metric series, unchanged from earlier releases); queries issued
natively through PgxAccess pools are not yet traced.
*/
package postgres
