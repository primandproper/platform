/*
Package authorization answers "may this principal do this thing".

Authentication establishes who is calling; this establishes what they may do.
The module has had the first half for a long time (see authentication) and none
of the second, which left the most consequential security decision in every
consuming service to be re-implemented per service.

# The seam

Authorization is two operations that look like one, and conflating them is the
mistake this package is shaped to avoid:

	resolve   role names -> permission set   once per session   may do I/O
	check     permission in set              many per request   never does

Only resolution is pluggable. PolicyResolver has two implementations —
authorization/static compiles the policy in, authorization/database stores it in
SQL — and both return the same *PermissionSet. Everything downstream is
identical whichever is configured, so moving between them is a configuration
change with no code change at any call site.

Checking is Grants.Has: one or two map lookups, no context, no error, no
allocation. That it cannot fail is a property worth defending. The obvious
alternative interface —

	Authorize(ctx, principal, action, resource) (bool, error)

— makes a map lookup and a network round trip indistinguishable at the call
site, which is how a permission check inside a loop becomes N round trips. It
adds an error branch with no cause, and the tempting way to handle "engine
unavailable" is to allow. And it takes a resource that nothing passes: instance
scoping is an indexed predicate in the query that was going to run anyway
(WHERE belongs_to_account = ...), not a second system to consult. A parameter
every caller passes zero to cannot be removed later; a method can always be
added.

# Principals

There is no Principal type here, because enforcement needs authority rather
than identity. Deriving authority from identity requires a store, which is what
drags I/O onto the check; that derivation is the session's job and happens once,
at login.

Consumers bridge their own session type with a GrantsExtractor. This is also
where a multi-scope model collapses: a service that separates service-wide
authority from per-tenant authority hands both sets to NewGrants and gets the
OR of them, without the platform needing to know that "tenant" exists.

	func extract(ctx context.Context) (authorization.Grants, bool) {
		s, err := sessions.FromContext(ctx)
		if err != nil {
			return authorization.Grants{}, false
		}

		return authorization.NewGrants(
			s.ServicePermissions,                    // may be nil
			s.AccountPermissions[s.ActiveAccountID], // absent key -> nil
		), true
	}

NewGrants drops nil sets, so an administrator acting on a tenant they do not
belong to simply carries one set instead of two. That case needs no branch
anywhere, which is the point: it is the case most likely to be forgotten.

# Choosing a backend

Start with authorization/static. It needs no database, no migrations, and no
configuration, and it is what an empty Provider selects. authorization/config
builds it, and applies the caching decorator, without naming a table at all.

The Provider itself lives in authorization/database/config, beside the store it
selects, along with the block that store reads. A deployment resolving policy
from declarations therefore configures it through authorization/config and one
resolving it from SQL through authorization/database/config; the second embeds
the first, so an operator's environment variables are the same either way. Both
packages' doc.go carry the reason.

Move to authorization/database when roles must become editable data — when an
operator has to define a new role, or change what one grants, without shipping a
release. Reassigning which roles a principal holds does not require it: role
assignments belong to the consumer in both cases, because they reference the
consumer's own users and tenants. This package owns policy, not assignment.

The same []Role seeds either one. authorization/database.Seed takes exactly what
static.NewResolver takes, and ValidateRoles runs in both, so a policy rejected
in one is rejected in the other and a code-side policy cannot quietly drift from
a database-side one.

No embedded policy engine and no external authorization service ships here. An
engine buys a policy language, which is worth having when policy has conditions
and wildcards, and is pure ceremony over role-to-permission tables that have
neither. A relationship service answers questions about resource graphs, which
is worth a network hop when resources are reachable through arbitrary
relationships, and is not when ownership is one indexed column. Either could
sit behind PolicyResolver later — that is what putting the seam there buys —
but shipping an adapter with no user is how a package ends up unused.

# Fail closed

An unconfigured resolver grants nothing: a static policy with no roles resolves
every role to the empty set, so the default configuration denies. Unknown role
names contribute nothing rather than erroring, so a principal still assigned a
role the policy has dropped loses that authority instead of losing the ability
to make requests.

Enforcement inverts the usual default too. In authorization/grpc an *undeclared*
method is denied, so forgetting to register a method fails closed; a method that
genuinely needs no authorization is declared Public, which is a statement rather
than an absence. That guarantee is not available over HTTP — route patterns are
not known before the mux matches, so authorization/http declares requirements at
registration instead and a route with no middleware is simply unguarded. See
that package for why, and for what to assert in a test instead.

The one deliberate hole is audit-only mode, which evaluates and records every
decision but denies nothing. Turning enforcement on across a service that never
had it is otherwise a coin flip on a large hand-written table. It is a code-level
option rather than configuration precisely because it disables enforcement:
that belongs in a diff, not in an environment variable.

# Where a permission set may live

A resolved PermissionSet may live in a cache. It may never live in a credential.

That is the invariant the whole design protects, and the reason resolution
rather than checking is the pluggable part. A cache entry that fails to decode
after a deploy degrades to a query; a session or token that fails to decode logs
its holder out. authorization/cached is therefore the only component whose
encoding can break on a version skew, and the only one where breaking is free —
its keys carry a format version and a decode failure is treated as a miss.
Nothing else here serializes a PermissionSet, which makes changing the
representation a non-event rather than a migration.

# Denials

Denials surface as ErrPermissionDenied, whose canonical declaration is in the
root errors package so that errors/http and errors/grpc can map it without
importing this one. Both already do: it becomes HTTP 403 with code E110, and
codes.PermissionDenied. A handler that returns it — wrapped or bare — gets the
right status with no status construction of its own.

The message a client sees is the constant "permission denied". Which permission
was missing goes to the span and stops there; naming it in the response
discloses the permission taxonomy to a caller who just failed to authorize.

An ordinary denial is not logged. It increments a denials counter and marks the
ambient span, and that is all: a caller lacking a permission is the enforcement
layer working, and the volume worth watching is a counter rather than a line
per request. The enforcers reserve their logger for the denials that mean
something is misconfigured — an undeclared gRPC method, a route registered with
an empty permission list, a request that reached enforcement with no grants at
all — because each of those is a bug in the wiring rather than in the caller.

# What is not here

Answering "which resources may this principal act on" is out of scope, and not
merely deferred. Doing it requires either mirroring every resource's ownership
into the authorization layer — dual writes, a consistency window against your
own transaction — or emitting SQL fragments, which couples this package to
column names and join shapes. Today that question is answered by a predicate
inside a query that already runs. Revisit only when a resource becomes reachable
by someone who is not a member of its owner, at which point the shape is a
filter fragment and it belongs next to the query, not here.
*/
package authorization
