/*
Package resend sends email through Resend.

Choosing it commits a deployment to a Resend account and one API token, which is
the whole of its configuration — there is no domain, region, or host to set. The
caller supplies the *http.Client the SDK will use, so timeouts and transport
instrumentation stay the caller's.

# What one send is

SendEmail delivers one HTML message to one recipient, passing the caller's
context through to the request, and returns when Resend has answered with the
message ID it assigned, which goes on the span. It is synchronous and does not
retry; a failed send scores the circuit breaker, and while that breaker is open
every call returns circuitbreaking.ErrCircuitBroken before touching the network.

Addresses are rendered through email.FormatAddress. That matters more here than
it looks: the API takes recipients as a list of mailbox strings, so an unescaped
comma in a display name would be read as a recipient separator.
*/
package resend
