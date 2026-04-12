package healthcheck

import (
	"context"
	"sync"
)

// Status represents component health status.
type Status string

const (
	StatusUp   Status = "up"
	StatusDown Status = "down"
)

// ComponentResult is the result of a single component check.
type ComponentResult struct {
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// Result is the aggregate result of all health checks.
type Result struct {
	Components map[string]ComponentResult `json:"components"`
	Status     Status                     `json:"status"`
}

// Checker performs a health check for a component.
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

// Registry holds checkers and runs them.
type Registry interface {
	Register(checker Checker)
	CheckAll(ctx context.Context) *Result
}

type registry struct {
	checkers []Checker
	mu       sync.RWMutex
}

// NewRegistry returns a new Registry.
func NewRegistry() Registry {
	return &registry{checkers: make([]Checker, 0)}
}

// Register adds a checker to the registry.
func (r *registry) Register(checker Checker) {
	if checker == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkers = append(r.checkers, checker)
}

// CheckAll runs all registered checkers and returns the aggregate result.
func (r *registry) CheckAll(ctx context.Context) *Result {
	r.mu.RLock()
	checkers := make([]Checker, len(r.checkers))
	copy(checkers, r.checkers)
	r.mu.RUnlock()

	result := &Result{
		Status:     StatusUp,
		Components: make(map[string]ComponentResult),
	}

	for _, c := range checkers {
		name := c.Name()
		err := c.Check(ctx)
		if err != nil {
			result.Components[name] = ComponentResult{Status: StatusDown, Message: err.Error()}
			result.Status = StatusDown
		} else {
			result.Components[name] = ComponentResult{Status: StatusUp}
		}
	}

	return result
}
