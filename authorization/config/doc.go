// Package authorizationcfg builds an authorization.PolicyResolver from
// configuration.
//
// What it builds is the static resolver, optionally wrapped in
// authorization/cached. Both are declarations: a set of roles that came from a
// build or a config file, and a decorator over whatever it was handed. Neither
// owns a table, and neither needs a database.Client, so a service that
// authorizes against policy it ships can wire this package and nothing else.
//
// The zero value works and grants nothing. That is deliberate on both counts:
// the most accessible implementation is the default so the package runs with no
// infrastructure, and an unconfigured authorization layer denies rather than
// admits.
//
// Because the caching decision is made here rather than by the caller, a process
// that edits policy reaches invalidation by asserting
// authorization.PolicyInvalidator on the returned resolver rather than by naming
// a concrete type.
//
// # Why there is no Provider field
//
// There used to be one, selecting between the static resolver and the SQL-backed
// one in authorization/database. It has moved, along with the database block it
// gated, to authzdbcfg — the config subpackage beside the store it builds.
//
// The rule behind the move is one sentence, and it is the same one applied to
// webauthncfg and oauth2servercfg: the provider string exists because a second
// implementation exists, so it belongs with the implementation that created the
// choice. Take the SQL resolver away and this package has exactly one thing to
// build, which is nothing to select between; a Provider field with one legal
// value is a field whose only reachable value is the default.
//
// The move was forced rather than chosen. authorization is a provider behind an
// interface and leaves for primitives-go; authorization/database owns a table
// and stays. A config that dispatched on a provider string named both, so it
// could travel with neither, and the module README's "Primitives and Domains"
// section is where that constraint is written down.
//
// # Why it was not the other two exits
//
// Keeping the config whole on the domain side would leave authorization
// shipping no config subpackage at all, against the convention every other
// package here follows — so a consumer wanting only the static resolver would
// import the module that owns the roles table to configure the one that does
// not. That cost is paid forever, by every wiring site, and it grows with each
// primitives-only consumer.
//
// Conceding that an interface with a table-owning implementation is a domain
// noun would keep authorization, webauthn and oauth2server here whole. It is
// coherent, and it is a bigger claim than this ticket: it moves four packages
// back across a line drawn deliberately, on the strength of one field.
//
// What would overturn the split is a second primitive implementation arriving
// here — a resolver reading policy from a file watcher, say. Then this package
// would have a genuine choice to make and would want its own provider string
// back, and the one in authzdbcfg would be selecting between this package's
// answer and its own.
//
// # The seam the two halves meet at
//
// authzdbcfg calls NewPolicyResolver for the branch it does not own, and
// NewCachedResolver for the one it does. The second is exported for that reason
// alone: it exists so that the CacheTTL read and the cached.NewResolver call
// have one home rather than two. A second copy on the database branch could
// drift — a different default, a dropped option, a TTL read off the wrong field
// — which is exactly the kind of duplication the module's CLAUDE.md rules out,
// as against duplication that can only be different.
package authorizationcfg
