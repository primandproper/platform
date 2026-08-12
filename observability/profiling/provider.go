// Package profiling is the seam for continuous profiling: the fourth
// observability pillar, and the one that is not a dependency of anything.
//
// Logging, tracing, and metrics are threaded into components as options, because
// each records what a particular component did. A profiler records what the
// process did, so nothing is instrumented with it — observability.Pillars holds
// one to start and stop it, and no constructor in this module takes one. That is
// why Pillars.Deps hands back three things and not four.
//
// Two implementations answer the interface and they differ in who does the
// asking. The pprof provider serves profiles from an HTTP endpoint, so somebody
// has to go and pull one while the interesting thing is still happening. The
// pyroscope provider pushes them continuously to a server, so the profile from
// the incident exists whether or not anyone thought to collect it during.
//
// A deployment that names neither gets neither. Absent is a supported answer
// here, not a degraded one: profiling is the pillar with a measurable cost, and
// running without it is a legitimate choice rather than a misconfiguration.
package profiling

import "context"

// Provider manages the lifecycle of continuous profiling.
// Start begins profiling; Shutdown stops it gracefully.
type Provider interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}
