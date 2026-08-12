package logging

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"
)

const (
	// LoggerNameKey is a key we can use to denote logger names across implementations.
	LoggerNameKey = "service_name"
)

const (
	// DebugLevel describes a debug-level log.
	DebugLevel Level = "debug"
	// InfoLevel describes an info-level log.
	InfoLevel Level = "info"
	// WarnLevel describes a warn-level log.
	WarnLevel Level = "warn"
	// ErrorLevel describes an error-level log.
	ErrorLevel Level = "error"
)

// AllLevels returns every level this package defines, in increasing severity.
func AllLevels() []Level {
	return []Level{DebugLevel, InfoLevel, WarnLevel, ErrorLevel}
}

type (
	// Level names the severity threshold a Logger emits at.
	//
	// It is a string-backed value type, so == compares the level rather than a
	// pointer, and a Level decoded from env or JSON equals the constant it names.
	// The zero value is the empty Level, which every implementation reads as
	// InfoLevel.
	Level string

	// RequestIDFunc fetches a string ID from a request.
	RequestIDFunc func(*http.Request) string
)

// String returns the level's name.
func (l Level) String() string {
	return string(l)
}

// Valid reports whether l names one of the levels this package defines. The
// zero value is not valid; implementations treat it as InfoLevel.
func (l Level) Valid() bool {
	switch l {
	case DebugLevel, InfoLevel, WarnLevel, ErrorLevel:
		return true
	default:
		return false
	}
}

// Logger represents a simple logging interface we can build wrappers around.
// NOTICE: someone, naive and green, may be enticed to add a method to this interface akin to:
// WithQueryFilter(*types.QueryFilter) Logger
// This is a fool's errand, it would introduce a disallowed import cycle.
type Logger interface {
	Info(string)
	Debug(string)
	Warn(string)
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

	return noopLoggerSingleton
}

// NewNamedLogger creates a named Logger from the given Logger.
// If logger is nil, a noop Logger is used.
func NewNamedLogger(logger Logger, name string) Logger {
	return EnsureLogger(logger).WithName(name)
}
