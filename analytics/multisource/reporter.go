package multisource

import (
	"context"
	"maps"

	"github.com/primandproper/platform-go/v10/analytics"
	"github.com/primandproper/platform-go/v10/analytics/noop"
	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/keys"
)

const (
	name = "multisource_event_reporter"
	// SourcePropertyKey is the event property used to identify the analytics source (e.g. ios, web).
	// For PostHog, where a single API key is shared across sources, this property distinguishes events.
	SourcePropertyKey = "source"
)

// MultiSourceEventReporter delegates events to per-source EventReporters. The reporters map is
// populated at construction and never mutated afterwards, so reads need no synchronization.
type MultiSourceEventReporter struct {
	o11y      observability.Observer
	reporters map[string]analytics.EventReporter
}

// NewMultiSourceEventReporter returns a new MultiSourceEventReporter.
func NewMultiSourceEventReporter(reporters map[string]analytics.EventReporter, opts ...Option) *MultiSourceEventReporter {
	o := newOptions(opts)

	if reporters == nil {
		reporters = make(map[string]analytics.EventReporter)
	}
	return &MultiSourceEventReporter{
		reporters: reporters,
		o11y:      observability.NewObserver(name, o.logger, o.tracerProvider),
	}
}

// getReporter returns the reporter for the source, or Noop if unknown/missing.
func (m *MultiSourceEventReporter) getReporter(source string) analytics.EventReporter {
	if r, ok := m.reporters[source]; ok && r != nil {
		return r
	}
	m.o11y.Logger().WithValue("source", source).WithValue("known_sources", m.knownSources()).Info("no analytics reporter configured for source, using noop")
	return noop.NewEventReporter()
}

func (m *MultiSourceEventReporter) knownSources() []string {
	sources := make([]string, 0, len(m.reporters))
	for k := range m.reporters {
		sources = append(sources, k)
	}
	return sources
}

// Close flushes and closes every underlying reporter. Reporters shared across multiple sources
// (e.g. PostHog sources with the same API key) are closed exactly once.
func (m *MultiSourceEventReporter) Close(ctx context.Context) error {
	// Every reporter is closed even if an earlier one fails, and the failures
	// are joined: one source's flush failing is no reason to leak the rest.
	var errs []error

	seen := make(map[analytics.EventReporter]struct{}, len(m.reporters))
	for source, r := range m.reporters {
		if r == nil {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}

		if err := r.Close(ctx); err != nil {
			errs = append(errs, errors.Wrapf(err, "closing reporter for source %q", source))
		}
	}

	return errors.Join(errs...)
}

// Shutdown implements do.Shutdowner so the DI container flushes buffered events
// on shutdown, and reports a failed final flush rather than swallowing it.
func (m *MultiSourceEventReporter) Shutdown(ctx context.Context) error {
	return m.Close(ctx)
}

// withSourceProperty returns a copy of properties with the source property set.
// For PostHog (single API key across sources), the source property distinguishes events.
func withSourceProperty(source string, properties map[string]any) map[string]any {
	merged := make(map[string]any, len(properties)+1)
	maps.Copy(merged, properties)
	merged[SourcePropertyKey] = source
	return merged
}

// TrackEvent records an event for an identified user.
func (m *MultiSourceEventReporter) TrackEvent(ctx context.Context, source, event, userID string, properties map[string]any) error {
	ctx, op := m.o11y.Begin(ctx,
		observability.WithValue("source", source),
		observability.WithValue("event", event),
		observability.WithValue("user_id", userID),
		observability.WithValue(keys.LengthKey, len(properties)),
	)
	defer op.End()

	return m.getReporter(source).EventOccurred(ctx, event, userID, withSourceProperty(source, properties))
}

// AddUser identifies a user against the reporter for the given source, forwarding the
// user's traits. Every underlying reporter supports identify via analytics.EventReporter.AddUser.
func (m *MultiSourceEventReporter) AddUser(ctx context.Context, source, userID string, properties map[string]any) error {
	ctx, op := m.o11y.Begin(ctx,
		observability.WithValue("source", source),
		observability.WithValue("user_id", userID),
		observability.WithValue(keys.LengthKey, len(properties)),
	)
	defer op.End()

	return m.getReporter(source).AddUser(ctx, userID, withSourceProperty(source, properties))
}

// TrackAnonymousEvent records an event for an anonymous user.
func (m *MultiSourceEventReporter) TrackAnonymousEvent(ctx context.Context, source, event, anonymousID string, properties map[string]any) error {
	ctx, op := m.o11y.Begin(ctx,
		observability.WithValue("source", source),
		observability.WithValue("event", event),
		observability.WithValue("anonymous_id", anonymousID),
		observability.WithValue(keys.LengthKey, len(properties)),
	)
	defer op.End()

	return m.getReporter(source).EventOccurredAnonymous(ctx, event, anonymousID, withSourceProperty(source, properties))
}
