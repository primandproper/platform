package profiling

import "context"

// NoopProvider is a no-op profiling provider.
func NewNoopProvider() Provider {
	return &noopProvider{}
}

type noopProvider struct{}

func (n *noopProvider) Start(context.Context) error {
	return nil
}

func (n *noopProvider) Shutdown(context.Context) error {
	return nil
}
