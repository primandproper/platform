/*
Package memory keeps an authorization server's state in maps.

	store := memory.NewStore(memory.WithSweeper(ctx, oauth2server.DefaultSweepInterval))
	srv, _ := oauth2server.NewServer(cfg, store, authenticator)

It exists for tests, for a single-process development server, and for nothing
else — and saying so is the point of the package rather than a caveat on it,
because a map-backed authorization server is exactly what a consumer assembling
this from the reference examples ends up with, and it looks like it works.

# What it cannot do

An authorization code is issued by the replica that served /authorize and
redeemed by whichever replica serves /token. With this store those are the same
process or the redemption fails, so a fleet behind a load balancer fails logins
in proportion to how well the balancer spreads them — which reads as a flaky
login rather than as a missing dependency.

A restart drops every registered client. Under RFC 7591 dynamic registration
that is every client there is, so a deploy invalidates the entire client
population and each one has to discover and re-register before its next login
works.

oauth2server/database is the answer to both, and it is the same interface.

# What it does do

Everything the interface promises, atomically, under one mutex. It passes the
same oauth2servertest suite the database store does, including the case where
two goroutines redeem one authorization code and exactly one of them wins. That
is why it is a usable test double rather than a shape that compiles: a suite
that schedules against this store inherits whatever it gets wrong, so it is held
to the same contract as the real thing.
*/
package memory
