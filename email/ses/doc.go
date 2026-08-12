/*
Package ses sends email through Amazon SES v2.

It is the only provider in this family whose credentials are not in its config.
Config names a region and nothing else; the identity SES is called with comes
from the ambient AWS credential chain — instance role, container role,
environment, shared profile — resolved at construction. Choosing SES therefore
commits a deployment to whatever IAM identity the process runs as, and to that
identity holding ses:SendEmail, rather than to a secret this module can be
handed. It also means credentials rotate underneath the Emailer without it being
rebuilt, which no key-bearing sibling does.

A pre-built SendEmailAPI may be passed instead, in which case it is used as-is
and the *http.Client is not consulted; that is the seam tests and callers with
their own SES client use. With no client, the *http.Client is required, since it
is what the SDK will be configured with.

# What one send is

SendEmail delivers one HTML message to one recipient using the simple content
shape — a subject and an HTML body — and returns when SES has answered with the
message ID it assigned, which goes on the span. Raw MIME, attachments, and
configuration sets are not exposed. It is synchronous and does not retry, beyond
whatever the AWS SDK does internally; a failed send scores the circuit breaker,
and while that breaker is open every call returns
circuitbreaking.ErrCircuitBroken before touching the network.

Addresses are rendered through email.FormatAddress, which quotes the display
name so a comma in it cannot inject recipients.
*/
package ses
