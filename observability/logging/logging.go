package logging

import (
	"log/slog"
	"net/http"
	"os"

	"go.opentelemetry.io/otel/trace"
)

const (
	// LoggerNameKey is a key we can use to denote logger names across implementations.
	LoggerNameKey = "service_name"
)

var (
	// InfoLevel describes a info-level log.
	InfoLevel Level = new(level("info"))
	// DebugLevel describes a debug-level log.
	DebugLevel Level = new(level("debug"))
	// ErrorLevel describes a error-level log.
	ErrorLevel Level = new(level("error"))
	// WarnLevel describes a warn-level log.
	WarnLevel Level = new(level("warn"))
)

func AllLevels() []Level {
	return []Level{InfoLevel, DebugLevel, ErrorLevel, WarnLevel}
}

type (
	level string

	// Level is a simple string alias for dependency injection's sake.
	Level *level

	// RequestIDFunc fetches a string ID from a request.
	RequestIDFunc func(*http.Request) string
)

// Logger represents a simple logging interface we can build wrappers around.
// NOTICE: someone, naive and green, may be enticed to add a method to this interface akin to:
// WithQueryFilter(*types.QueryFilter) Logger
// This is a fool's errand, it would introduce a disallowed import cycle.
type Logger interface {
	Info(string)
	Debug(string)
	Error(whatWasHappeningWhenErrorOccurred string, err error)

	SetRequestIDFunc(RequestIDFunc)

	Clone() Logger
	WithName(string) Logger
	WithValues(map[string]any) Logger
	WithValue(string, any) Logger
	WithRequest(*http.Request) Logger
	WithResponse(response *http.Response) Logger
	WithError(error) Logger
	WithSpan(span trace.Span) Logger
}

// EnsureLogger guarantees that a Logger is available.
func EnsureLogger(logger Logger) Logger {
	if logger != nil {
		return logger
	}

	return NewNoopLogger()
}

// NewNamedLogger creates a named Logger from the given Logger.
// If logger is nil, a noop Logger is used.
func NewNamedLogger(logger Logger, name string) Logger {
	return EnsureLogger(logger).WithName(name)
}

func init() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
}
