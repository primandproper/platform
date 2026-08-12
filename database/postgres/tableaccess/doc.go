/*
Package tableaccess is the PostgreSQL database.Manager: the administrative
surface that creates roles and databases and grants table privileges, as
distinct from the query path a database.Client serves.

It is provisioning code. The calls here are the ones a service makes when it
stands up a tenant's database or hands a new role the privileges it needs, not
ones a request handler makes.

# Why there is one of these per dialect

None of this is portable SQL. CREATE USER, GRANT, and the catalog queries that
answer "does this role exist" differ per engine, and — the part that decides the
shape of the code — identifiers and role names cannot travel as bind parameters
at all, because these are utility statements. Every dialect therefore has to
solve credential handling its own way, and its own way is what each of these
packages is.

Here that means the credential never appears in statement text. CREATE USER runs
inside a transaction that first stashes the name and password in
transaction-local settings as bind parameters, then executes a constant DO block
which reads them back and lets the server apply its own identifier and literal
quoting. The point is otelsql: it copies statement text onto a span attribute
when query logging is on, and a directly interpolated password would ride out to
whatever consumes those spans. set_config's local flag scopes the settings to the
transaction, so nothing survives on the pooled connection for the next caller to
read.

Where a name is interpolated rather than bound — DROP USER, CREATE DATABASE,
GRANT — it goes through the dialect's identifier quoting, and the privilege name
is checked against this package's own constants rather than passed through.

A name already taken comes back wrapping database.ErrUserAlreadyExists,
recognized by the SQLSTATE Postgres raises for a duplicate object, so errors/http
and errors/grpc render it as a conflict rather than a 500. The driver's error
stays underneath it.

Nothing on a span or in a log here carries a password.
*/
package tableaccess
