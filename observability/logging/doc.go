/*
Package logging is the Logger seam every package in this module writes through,
and the noop that stands in when a caller supplies none.

Four backends implement it — slog, zap, zerolog, and otelgrpc, the last of which
also ships records to an OTLP collector — and which one a deployment gets is
configuration rather than a code change. Constructors take a Logger as a WithX
option and never require one: EnsureLogger resolves a nil Logger to a noop, so a
service that names no logger is a service that logs nothing, not one that fails
to build.

# The interface is narrow on purpose

There are four emit methods — one per level — and a set of With* methods that
derive a logger carrying more context. Error takes what was being attempted
alongside the error, because "what failed" without "what was being tried" is the
message that turns up in a search and answers nothing. Warn, like Info and Debug,
takes only a message: a warning that has an error to report is an Error, and one
that does not is a message. There are no formatting variants: a message is a
constant and the variable parts are values, which is what makes a log line
groupable after the fact.

Adding a method that takes a domain type is the mistake the interface exists to
prevent — everything logs, so everything imports this package, and a method
naming a type from elsewhere in the module makes that an import cycle.

# What With* returns, and what that costs

Every With* method and WithName return a logging.Logger, because that is what the
interface says. A backend's own methods are therefore reachable on the value its
constructor returned and not on anything derived from it: zap's SetLevel is the
one that bites, since the derived loggers share the same atomic level and
re-leveling still works — but only through the *zap.Logger the constructor handed
back. Hold on to that value if a deployment intends to re-level at runtime.

Level is a string-backed type, so a level decoded from an environment variable
equals the constant it names. The zero value is not a valid level; every backend
reads it as InfoLevel.

Every level names both a threshold and a method, WarnLevel included: configuring
WarnLevel drops Info and Debug and keeps Warn and Error, and each backend maps it
onto that backend's own warn level rather than onto info or error.
*/
package logging
