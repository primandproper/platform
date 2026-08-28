package sessions

// Observability keys for this package's spans and log fields. Namespaced so an
// attribute cannot collide with another component writing to the same trace.
//
// Note what is absent: the identifier itself. A session identifier is a bearer
// credential, and a trace or a log line is exactly the place it must not be —
// exported to a vendor, retained for weeks, readable by anyone with dashboard
// access. Everything here describes a session without naming it.
const (
	// The holder's two halves. A scope and a principal identifier are not
	// bearer credentials — they name who a session is for, not how to present
	// it — which is what separates them from the one value this package never
	// attaches.
	scopeKey     = "sessions.scope"
	principalKey = "sessions.principal"
	// listedKey and revokedKey are how many sessions came back from an
	// enumeration and how many a revocation ended.
	listedKey  = "sessions.listed"
	revokedKey = "sessions.revoked"

	createdAtKey     = "sessions.created_at"
	lastSeenAtKey    = "sessions.last_seen_at"
	expiresAtKey     = "sessions.expires_at"
	touchedKey       = "sessions.touched"
	expiryKey        = "sessions.expiry"
	recordVersionKey = "sessions.record_version"
	operationKey     = "operation"
	reasonKey        = "reason"
)

// Operations reported on sessions_latency_ms.
const (
	operationNew    = "new"
	operationGet    = "get"
	operationSave   = "save"
	operationRenew  = "renew"
	operationDelete = "delete"
	operationList   = "list"
	operationRevoke = "revoke"
)

// Reasons reported on sessions_revoked: which of the three revocations ended a
// session.
const (
	revocationOne       = "one"
	revocationAll       = "all"
	revocationAllExcept = "all_but_kept"
)
