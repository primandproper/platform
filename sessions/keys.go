package sessions

// Observability keys for this package's spans and log fields. Namespaced so an
// attribute cannot collide with another component writing to the same trace.
//
// Note what is absent: the identifier itself. A session identifier is a bearer
// credential, and a trace or a log line is exactly the place it must not be —
// exported to a vendor, retained for weeks, readable by anyone with dashboard
// access. Everything here describes a session without naming it.
const (
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
)
