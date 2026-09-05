/*
Package grpc translates errors into gRPC statuses, and back again on the other
side of the wire.

MapToGRPC resolves a Go error to a codes.Code, consulting PlatformMapper first
and then whatever mappers domains have registered. The interceptors apply that
mapping to whatever a handler returns, and DecodeErrorFromStatus reconstructs the
original error on the client, so errors.Is keeps matching across a service
boundary.

The sentinel set mapped here is the same one errors/http maps, deliberately. A
service exposing both transports would otherwise answer one failure with a
considered status on one and codes.Unknown on the other, and which the client got
would depend on how it happened to connect.

# Which direction the imports run

This package imports the packages whose sentinels it maps — circuitbreaking,
database, idempotency, links, ratelimiting, sessions, and the rest. That is what
lets the mapping live in one place instead of being restated at every service
implementation. It also fixes the dependency direction: nothing in those packages
may import errors/grpc back. A package that finds itself wanting a codes.Code
wants a sentinel of its own, mapped here.

Domains outside this module register their own mappers with
RegisterGRPCErrorMapper, usually from an init function. The platform mapper is
consulted first, registered mappers after, in registration order.

# What reaches the client, and what that assumes

The status message is derived from the code rather than from the error's text,
which is the whole wrapped chain and can name tables, connection strings, and the
permission that was missing. The exception is a list of platform sentinels
documented as client-safe, whose own wording tells a caller what to do
differently without describing the policy behind the refusal.

The full error does still cross the wire, encoded in the status details, and that
is what makes the error reconstructable on the far side. It is meant for trusted
service-to-service traffic. A server running these interceptors and reachable by
untrusted clients needs that detail stripped at the edge — otherwise the internal
error text this package took care to keep out of the message is available in the
details of the same response.
*/
package grpc

//platform:transport mapping: a sentinel to a gRPC code, and back
