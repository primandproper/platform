/*
Package mailjet sends email through Mailjet.

Choosing it commits a deployment to a Mailjet account and to both halves of its
credential pair — the public API key and the secret key. Either one missing is a
construction error. The caller supplies the *http.Client the SDK will use, so
timeouts and transport instrumentation stay the caller's.

# What one send is

SendEmail delivers one HTML message to one recipient through the v3.1 send API
and returns when Mailjet has answered. It is synchronous and does not retry; a
failed send scores the circuit breaker, and while that breaker is open every
call returns circuitbreaking.ErrCircuitBroken before touching the network.

Unlike most of its siblings this package does not call email.FormatAddress. The
v3.1 payload carries the display name and the address as separate JSON fields,
so there is no header for a comma in a name to split and nothing to escape;
joining them into one RFC 5322 mailbox would put the quoting inside the value
Mailjet treats as the name.

Mailjet's response carries per-message status, and this package discards it: the
send is reported as an error only when the API call itself fails.
*/
package mailjet
