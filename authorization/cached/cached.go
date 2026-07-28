// Package cached wraps any authorization.PolicyResolver in a cache.
//
// It is a decorator rather than a third backend: it composes with static (where
// it is redundant) and with database (where it is the difference between a
// query per session build and a query per policy change).
//
// The cache is keyed by role names, not by principal. That is what makes it
// worth having — a deployment with five roles has five hot entries shared by
// every principal, rather than one per user, so the hit rate approaches one and
// the memory cost does not grow with traffic.
//
// # Why caching is safe here and would not be elsewhere
//
// A resolved PermissionSet may live in a cache. It may never live in a
// credential. That distinction is the reason this package's seam sits at the
// resolver: a cache entry that fails to decode after a deploy degrades to a
// query, while a session or token that fails to decode logs its holder out.
// This package is consequently the only place in authorization where an
// encoding change can bite, and the only place where being bitten costs
// nothing — keys carry a format version, and any decode failure is treated as a
// miss rather than an error.
//
// The cost of caching is staleness: a policy edit takes effect when the entry
// expires, not immediately. Invalidate narrows that window in the process that
// made the edit; other replicas wait out the TTL. Set the TTL to the longest
// delay you would accept between revoking a role's authority and it taking
// effect everywhere.
package cached

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/primandproper/platform-go/v8/authorization"
	"github.com/primandproper/platform-go/v8/cache"
	platformerrors "github.com/primandproper/platform-go/v8/errors"
	"github.com/primandproper/platform-go/v8/observability/logging"
)

// serviceName names the Resolver's logger.
const serviceName = "authorization_cached"

// keyFormatVersion prefixes every cache key.
//
// Bump it whenever PermissionSet's encoding changes. Entries written by an
// older binary then become unreachable rather than mis-decoded, and the cost of
// that is one round of misses.
const keyFormatVersion = "authzv1"

// DefaultTTL is the entry lifetime when WithTTL is not supplied.
const DefaultTTL = 5 * time.Minute

var _ authorization.PolicyResolver = (*Resolver)(nil)

// Resolver caches the results of an inner PolicyResolver.
type Resolver struct {
	inner  authorization.PolicyResolver
	cache  cache.Cache[authorization.PermissionSet]
	logger logging.Logger

	generation atomic.Uint64
	ttl        time.Duration
}

// Option configures a Resolver.
type Option func(*Resolver)

// WithLogger attaches a logger. Cache faults are logged; hits and misses are not.
func WithLogger(logger logging.Logger) Option {
	return func(r *Resolver) {
		r.logger = logger
	}
}

// WithTTL sets the entry lifetime. A non-positive value uses DefaultTTL.
func WithTTL(ttl time.Duration) Option {
	return func(r *Resolver) {
		if ttl > 0 {
			r.ttl = ttl
		}
	}
}

// NewResolver wraps inner with c.
func NewResolver(
	inner authorization.PolicyResolver,
	c cache.Cache[authorization.PermissionSet],
	opts ...Option,
) (*Resolver, error) {
	if inner == nil {
		return nil, platformerrors.Wrap(platformerrors.ErrNilInputParameter, "inner policy resolver")
	}
	if c == nil {
		return nil, platformerrors.Wrap(platformerrors.ErrNilInputParameter, "cache")
	}

	r := &Resolver{inner: inner, cache: c, ttl: DefaultTTL}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}

	r.logger = logging.EnsureLogger(r.logger).WithName(serviceName)

	return r, nil
}

// PermissionsForRoles returns the cached resolution for roles, resolving and
// storing it on a miss.
//
// A cache fault — unreachable backend, undecodable entry — is logged and
// treated as a miss. Authorization must not fail because a cache did: the inner
// resolver is still authoritative and still reachable, so degrading to it is
// both correct and the only answer that keeps requests flowing.
func (r *Resolver) PermissionsForRoles(ctx context.Context, roles ...string) (*authorization.PermissionSet, error) {
	if len(roles) == 0 {
		return authorization.NewPermissionSet(), nil
	}

	key := r.key(roles)

	cached, err := r.cache.Get(ctx, key)
	switch {
	case err == nil && cached != nil:
		return cached, nil
	case err != nil && !errors.Is(err, cache.ErrNotFound):
		r.logger.WithValue("cache.key", key).Error("reading cached policy resolution", err)
	}

	set, err := r.inner.PermissionsForRoles(ctx, roles...)
	if err != nil {
		return nil, err
	}

	if err = r.cache.Set(ctx, key, set, cache.WithExpiry(r.ttl)); err != nil {
		// The answer is already correct; failing to memoize it is not a reason
		// to fail the caller.
		r.logger.WithValue("cache.key", key).Error("caching policy resolution", err)
	}

	return set, nil
}

// Roles delegates without caching. It serves admin tooling rather than the
// request path, and it is exactly the call an operator makes to confirm an edit
// landed — answering it from a cache would show them the state they were trying
// to change.
func (r *Resolver) Roles(ctx context.Context) ([]authorization.Role, error) {
	return r.inner.Roles(ctx)
}

// Invalidate drops the cached resolution for an exact set of roles.
func (r *Resolver) Invalidate(ctx context.Context, roles ...string) error {
	if len(roles) == 0 {
		return nil
	}
	if err := r.cache.Delete(ctx, r.key(roles)); err != nil {
		return platformerrors.Wrap(err, "invalidating cached policy resolution")
	}

	return nil
}

// InvalidateAll makes every entry this Resolver previously wrote unreachable.
//
// It bumps a generation counter held in memory, so it takes effect immediately
// in this process and not at all in others — the stale entries elsewhere expire
// on their TTL. Call it after a policy write so that the process that made the
// change stops serving what it just replaced; do not mistake it for a
// fleet-wide flush.
func (r *Resolver) InvalidateAll() {
	r.generation.Add(1)
}

// key builds the cache key for a role set: format version, generation, then the
// sorted role names. Sorting means the same roles in a different order share an
// entry.
func (r *Resolver) key(roles []string) string {
	sorted := slices.Clone(roles)
	slices.Sort(sorted)

	var b strings.Builder
	b.WriteString(keyFormatVersion)
	b.WriteByte(':')
	b.WriteString(strconv.FormatUint(r.generation.Load(), 10))
	b.WriteByte(':')
	b.WriteString(strings.Join(sorted, "\x00"))

	return b.String()
}
