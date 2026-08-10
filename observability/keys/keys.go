package keys

const (
	idSuffix = ".id"

	// NameKey is the standard key for referring to a name.
	NameKey = "name"
	// SpanIDKey is the standard key for referring to a span ID.
	SpanIDKey = "span" + idSuffix
	// TraceIDKey is the standard key for referring to a trace ID.
	TraceIDKey = "trace" + idSuffix
	// FilterCreatedAfterKey is the standard key for referring to a types.QueryFilter's CreatedAfter field.
	FilterCreatedAfterKey = "query_filter.created_after"
	// FilterCreatedBeforeKey is the standard key for referring to a types.QueryFilter's CreatedBefore field.
	FilterCreatedBeforeKey = "query_filter.created_before"
	// FilterUpdatedAfterKey is the standard key for referring to a types.QueryFilter's UpdatedAfter field.
	FilterUpdatedAfterKey = "query_filter.updated_after"
	// FilterUpdatedBeforeKey is the standard key for referring to a types.QueryFilter's UpdatedAfter field.
	FilterUpdatedBeforeKey = "query_filter.updated_before"
	// FilterSortByKey is the standard key for referring to a types.QueryFilter's SortBy field.
	FilterSortByKey = "query_filter.sort_by"
	// FilterCursorKey is the standard key for referring to a types.QueryFilter's next cursor.
	FilterCursorKey = "query_filter.cursor"
	// FilterLimitKey is the standard key for referring to a types.QueryFilter's limit.
	FilterLimitKey = "query_filter.limit"
	// FilterIsNilKey is the standard key for referring to a types.QueryFilter's null status.
	FilterIsNilKey = "query_filter.is_nil"
	// URLKey is the standard key for referring to a URL.
	URLKey = "url"
	// RequestHeadersKey is the standard key for referring to a http.Request's Headers.
	RequestHeadersKey = "request.headers"
	// RequestIDKey is the standard key for referring to a http.Request's ID.
	RequestIDKey = "request" + idSuffix
	// RequestMethodKey is the standard key for referring to a http.Request's Method.
	RequestMethodKey = "request.method"
	// RequestURIKey is the standard key for referring to a http.Request's URI.
	RequestURIKey = "request.uri"
	// ResponseStatusKey is the standard key for referring to a http.Request's status.
	ResponseStatusKey = "response.status"
	// ResponseHeadersKey is the standard key for referring to a http.Response's Headers.
	ResponseHeadersKey = "response.headers"
	// ReasonKey is the standard key for referring to a reason for a change.
	ReasonKey = "reason"
	// URLQueryKey is the standard key for referring to a URL query.
	URLQueryKey = "url.query"
	// SearchQueryKey is the standard key for referring to a search query parameter value.
	SearchQueryKey = "search_query"
	// UserAgentOSKey is the standard key for referring to a user agent's OS.
	UserAgentOSKey = "os"
	// UserAgentBotKey is the standard key for referring to a user agent's bot status.
	UserAgentBotKey = "is_bot"
	// UserAgentMobileKey is the standard key for referring to user agent's mobile status.
	UserAgentMobileKey = "is_mobile"
	// ValidationErrorKey is the standard key for referring to a struct validation error.
	ValidationErrorKey = "validation_error"
	// IndexNameKey is the standard key for referring to a given search index.
	IndexNameKey = "index.name"
	// UseDatabaseKey is the standard key for referring to whether or not the database was used in search.
	UseDatabaseKey = "use_database"

	// RequesterIDKey is the standard key for referring to a requesting user's ID (session/request context).
	RequesterIDKey = "request.made_by"
	// ActiveAccountIDKey is the standard key for referring to an active account ID (session context).
	ActiveAccountIDKey = "active_account" + idSuffix
	// UserIsServiceAdminKey is the standard key for referring to a user's admin status (session context).
	UserIsServiceAdminKey = "user.is_admin"
	// UserIDKey is the standard key for referring to a user ID (request/session context).
	UserIDKey = "user" + idSuffix
	// UsernameKey is the standard key for referring to a username (request context).
	UsernameKey = "user.username"

	// AuthorizationMethodKey is the standard key for referring to the route or RPC
	// method an authorization decision was made for.
	AuthorizationMethodKey = "authorization.method"
	// AuthorizationRequiredKey is the standard key for referring to the permissions
	// a route or method required. It belongs on the span and the log, never on a
	// response: it describes the policy, not the requester.
	AuthorizationRequiredKey = "authorization.required"
	// AuthorizationDecisionKey is the standard key for referring to the outcome of
	// an authorization check ("allowed", "denied", "audited").
	AuthorizationDecisionKey = "authorization.decision"
	// AuthorizationRolesKey is the standard key for referring to the role names a
	// policy resolution was performed for. Its value is always the names
	// themselves; use AuthorizationRoleCountKey when only a count is on hand, so
	// that one attribute never carries two types.
	AuthorizationRolesKey = "authorization.roles"
	// AuthorizationRoleCountKey is the standard key for referring to a number of
	// roles, for the bulk operations where naming each one would be unbounded.
	AuthorizationRoleCountKey = "authorization.role_count"
	// AuthorizationCacheOutcomeKey is the standard key for referring to how a
	// cached policy resolution was served ("hit", "miss", "fault"). "fault"
	// records that the cache was unusable and the resolution degraded to the
	// authoritative resolver — a successful request that silently cost a query.
	AuthorizationCacheOutcomeKey = "authorization.cache_outcome"

	// EmailSubjectKey is the standard key for referring to an outbound email's subject.
	EmailSubjectKey = "email.subject"
	// EmailToAddressKey is the standard key for referring to an outbound email's recipient address.
	EmailToAddressKey = "email.to_address"
	// EmailFromAddressKey is the standard key for referring to an outbound email's sender address.
	EmailFromAddressKey = "email.from_address"
	// FilenameKey is the standard key for referring to a filename.
	FilenameKey = "filename"
	// LengthKey is the standard key for referring to a requested or measured length.
	LengthKey = "length"
	// ConnectionURLKey is the standard key for referring to a datastore connection URL.
	ConnectionURLKey = "connection_url"
	// TopicKey is the standard key for referring to a message queue topic.
	TopicKey = "topic"
	// LockKeyKey is the standard key for referring to a distributed lock's name.
	// Every lock provider attaches it, so a trace query can filter on one
	// attribute regardless of which backend served the lock.
	LockKeyKey = "lock.key"
	// LockTTLKey is the standard key for referring to a distributed lock's TTL.
	LockTTLKey = "lock.ttl"
	// LockIDKey is the standard key for referring to a distributed lock's
	// backend-level identifier, such as a Postgres advisory lock ID.
	LockIDKey = "lock" + idSuffix
	// ServerAddressKey is the standard key for referring to the host and port an
	// outbound request was addressed to. It is deliberately the host rather than
	// the URL: a metric attribute has to stay bounded, and a path with an ID in
	// it does not.
	ServerAddressKey = "server.address"
	// RequestAttemptKey is the standard key for referring to which attempt of a
	// retried request produced what is being reported, counting from one.
	RequestAttemptKey = "request.attempt"
	// RequestAttemptsKey is the standard key for referring to how many attempts a
	// retried request took in total.
	RequestAttemptsKey = "request.attempts"
	// OutcomeKey is the standard key for referring to how a component judged the
	// result of an operation, as distinct from what the operation returned.
	OutcomeKey = "outcome"
	// RetryAfterKey is the standard key for referring to the delay a server asked
	// for via the Retry-After header.
	RetryAfterKey = "retry_after"
	// RateLimitKeyKey is the standard key for referring to what a rate limiter
	// counted a request against — a principal, an API key's hash, an address.
	//
	// It belongs on spans, never on metric attributes: it is unbounded by
	// construction, so one time series per caller is one time series too many.
	RateLimitKeyKey = "rate_limit.key"
	// RateLimitMethodKey is the standard key for referring to the RPC a rate
	// limiter ruled on. Unlike RateLimitKeyKey it comes from the service
	// definition rather than from the caller, so it is bounded and safe as a
	// metric attribute.
	RateLimitMethodKey = "rate_limit.method"
	// CacheOutcomeKey is the standard key for referring to how a cache answered:
	// a hit, a miss, a revalidation, or a request it took no part in. It is
	// deliberately distinct from OutcomeKey, which may describe the same
	// operation from another layer's point of view — an outbound request can be
	// both a cache miss and a circuit-breaker success, and one attribute cannot
	// carry both.
	CacheOutcomeKey = "cache.outcome"
	// SignatureSchemeKey is the standard key for referring to the request-signing
	// scheme a signature was minted or checked under. It comes from the
	// component's own configuration rather than from the caller, so it is bounded
	// and safe as a metric attribute.
	SignatureSchemeKey = "signature.scheme"
	// SecretNameKey is the standard key for referring to the secret a source was
	// asked for. Every secrets provider attaches it under this name — the four
	// of them previously used three between them — so a query for "who read this
	// secret" does not have to know which backend served it.
	//
	// The value is the name, never the secret. Providers that address a secret in
	// two parts attach SecretEntryKey alongside it.
	SecretNameKey = "secret.name"
	// SecretEntryKey is the standard key for referring to the entry within a
	// secret, for the providers whose secrets are maps rather than values — a
	// Kubernetes Secret's data key, for instance. It names the field, not its
	// contents.
	SecretEntryKey = "secret.entry"
	// EmbeddingModelKey is the standard key for referring to the model an
	// embedding was requested from. Comparing latency or cost across providers is
	// the ordinary reason to look, and a key that differed by a letter between
	// them made that a join nobody could write.
	EmbeddingModelKey = "embedding.model"
	// ChannelKey is the standard key for referring to the channel an async
	// notification was published to. It is distinct from TopicKey, which names a
	// message queue's topic: a channel is a fan-out address a client subscribes
	// to, and a service commonly has both.
	ChannelKey = "notification.channel"
	// EventTypeKey is the standard key for referring to the type of an async
	// notification's event. It is bounded by the publishing service's own
	// vocabulary, so it is safe as a metric attribute.
	EventTypeKey = "notification.event_type"
	// MemberIDKey is the standard key for referring to one subscriber of an async
	// notification channel.
	MemberIDKey = "notification.member" + idSuffix
)
