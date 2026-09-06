/*
Package grpc is the wire conversion for filtering's two types: the QueryFilter
a caller sends and the Pagination they are answered with, to and from the
generated messages in filtering/filteringpb.

It exists so that the rules do not cross the wire as a comment. A service
decoding a QueryFilter out of protobuf has to narrow to uint16 and has to
supply a default, and both are easy to get subtly wrong in a way nothing
reports: narrow the page size before clamping it and a requested 70000 wraps
to 4464, which the clamp then trims to a legible-looking page nobody asked
for. FromProto applies the ceiling to the wide value protobuf actually
carries, which is the only order that works, and then normalizes — so a
filter decoded here is the same filter the HTTP path would have produced,
without this package's numbers being restated anywhere a consumer keeps them.

It is a subpackage rather than part of filtering itself because filtering
builds no SQL, touches no database, and should take on no protobuf runtime
either. A consumer with no gRPC surface never links any of this.

Errors are reported rather than corrected, and the value is usable either
way. Every field is attempted, everything that decoded is applied, and the
error joins whatever did not — so a caller that logs and lists anyway gets
the page it would have got before, and one that would rather not answer a
mistyped filter has an error wrapping errors.ErrUnrecognizedInputValue, which
errors/http and errors/grpc already render as a 400 and InvalidArgument.
*/
package grpc

//platform:transport wire conversion: `QueryFilter` and `Pagination` to their generated messages
