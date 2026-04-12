package logging

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"
)

// noopLogger is a default Logger we can provide that does nothing in case of dire emergencies.
type noopLogger struct{}

var logger = new(noopLogger)

// NewNoopLogger provides our noop Logger to dependency manager.
func NewNoopLogger() Logger { return logger }

// Info satisfies our interface.
func (l *noopLogger) Info(string) {}

// Debug satisfies our interface.
func (l *noopLogger) Debug(string) {}

// Error satisfies our interface.
func (l *noopLogger) Error(string, error) {}

// Fatal satisfies our interface.
func (l *noopLogger) Fatal(error) {}

// Printf satisfies our interface.
func (l *noopLogger) Printf(string, ...any) {}

// SetLevel satisfies our interface.
func (l *noopLogger) SetLevel(_ Level) {}

// SetRequestIDFunc satisfies our interface.
func (l *noopLogger) SetRequestIDFunc(RequestIDFunc) {}

// WithName satisfies our interface.
func (l *noopLogger) WithName(string) Logger { return l }

// Clone satisfies our interface.
func (l *noopLogger) Clone() Logger { return l }

// WithValues satisfies our interface.
func (l *noopLogger) WithValues(map[string]any) Logger { return l }

// WithValue satisfies our interface.
func (l *noopLogger) WithValue(string, any) Logger { return l }

// WithRequest satisfies our interface.
func (l *noopLogger) WithRequest(*http.Request) Logger { return l }

// WithResponse satisfies our interface.
func (l *noopLogger) WithResponse(*http.Response) Logger { return l }

// WithError satisfies our interface.
func (l *noopLogger) WithError(error) Logger { return l }

// WithSpan satisfies our interface.
func (l *noopLogger) WithSpan(trace.Span) Logger { return l }
