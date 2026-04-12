package profiling

import "context"

// Provider manages the lifecycle of continuous profiling.
// Start begins profiling; Shutdown stops it gracefully.
type Provider interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}
