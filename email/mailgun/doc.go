/*
Package mailgun sends email through Mailgun.

Choosing it commits a deployment to a Mailgun account, a sending domain, and
that domain's private API key — both required, both checked at construction.
The caller supplies the *http.Client the SDK will use, so timeouts, proxying,
and transport instrumentation stay the caller's.

Which Mailgun to talk to is Config.BaseURL. Left empty it is the SDK's default
host, which is the US region; an account provisioned in the EU must set it to
BaseURLEU, since an EU domain does not exist under the US base and every send
fails on a domain that is plainly there in the dashboard. The same field points
the client at a test server.

# What one send is

SendEmail delivers one HTML message to one recipient and returns when Mailgun
has answered. It is synchronous and does not retry; a failed send scores the
circuit breaker, and while that breaker is open every call returns
circuitbreaking.ErrCircuitBroken before touching the network. Addresses are
rendered through email.FormatAddress, which quotes the display name so a comma
in it cannot inject recipients.

Mailgun's response carries the message ID it assigned, which goes on the span as
email.message_id — the same key the postmark, resend, and ses siblings use, so a
send here is traceable into the provider's own logs from what this package
records, and by the same query across providers.
*/
package mailgun
