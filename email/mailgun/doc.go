/*
Package mailgun sends email through Mailgun.

Choosing it commits a deployment to a Mailgun account, a sending domain, and
that domain's private API key — both required, both checked at construction.
The caller supplies the *http.Client the SDK will use, so timeouts, proxying,
and transport instrumentation stay the caller's.

Nothing here sets Mailgun's API base, so the SDK's default host is where mail
goes. An account provisioned in Mailgun's EU region is not reachable through
this package.

# What one send is

SendEmail delivers one HTML message to one recipient and returns when Mailgun
has answered. It is synchronous and does not retry; a failed send scores the
circuit breaker, and while that breaker is open every call returns
circuitbreaking.ErrCircuitBroken before touching the network. Addresses are
rendered through email.FormatAddress, which quotes the display name so a comma
in it cannot inject recipients.

Mailgun's response carries a message ID, and this package discards it — unlike
the postmark, resend, and ses siblings, which put it on the span. A send here is
therefore not traceable into the provider's own logs from what this package
records.
*/
package mailgun
