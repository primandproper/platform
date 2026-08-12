/*
Package tableaccess is the MySQL database.Manager: the administrative surface
that creates users and databases and grants table privileges, as distinct from
the query path a database.Client serves.

It is provisioning code. The calls here are the ones a service makes when it
stands up a tenant's database or hands a new user the privileges it needs, not
ones a request handler makes.

# Why there is one of these per dialect

None of this is portable SQL. CREATE USER, GRANT, and the catalog queries that
answer "does this user exist" differ per engine, and — the part that decides the
shape of the code — user names and identifiers cannot travel as bind parameters
at all, because these are utility statements. Every dialect therefore has to
solve credential handling its own way, and its own way is what each of these
packages is.

Here that means the credential never appears in statement text. CREATE USER binds
the name and password into session variables, assembles the statement server-side
with MySQL's own QUOTE, and runs it as a prepared statement, so every string that
goes over the wire is a constant. The point is otelsql: it copies statement text
onto a span attribute when query logging is on, and a directly interpolated
password would ride out to whatever consumes those spans.

MySQL has no transactional DDL to lean on the way the Postgres sibling does, and
session variables outlive the statement that set them, so the whole sequence pins
one connection and clears the variables before returning it to the pool.
Literal quoting done in Go doubles backslashes as well as quotes, which MySQL
needs and standard SQL does not; it assumes the default SQL mode, with
NO_BACKSLASH_ESCAPES off.

A name already taken comes back wrapping database.ErrUserAlreadyExists, but only
after a read confirms it. MySQL spends one error number on every CREATE USER it
declines to elaborate on, so the number alone is grounds to check rather than a
diagnosis — and a check that itself fails leaves the original error intact rather
than reporting a collision nobody established.

Nothing on a span or in a log here carries a password.
*/
package tableaccess
