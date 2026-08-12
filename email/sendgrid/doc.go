/*
Package sendgrid sends email through SendGrid.

Choosing it commits a deployment to a SendGrid account and one API token. The
caller's *http.Client is what actually issues the request — the SDK's own client
supplies the request template and nothing else — so timeouts and transport
instrumentation stay the caller's.

# Success is a status code, and failure is not always distinguishable

SendGrid answers a send with 202 Accepted. This package treats any other status
as a failure and returns ErrSendgridAPIResponse carrying the code, because the
SDK reports transport errors and rejections through the same nil-error path.

The limit of that is worth knowing before choosing this provider: when an
account is suspended or rate-limited to the point of not sending, the response
carries no distinguishing feature. A send that will never be delivered can
answer exactly like one that will, and no error is available to raise. If
knowing whether mail actually left matters, it has to come from SendGrid's own
event webhooks rather than from this call.

# What one send is

SendEmail delivers one HTML message to one recipient, passing the caller's
context through to the request. It is synchronous and does not retry; a failed
send — transport error or unexpected status — scores the circuit breaker, and
while that breaker is open every call returns circuitbreaking.ErrCircuitBroken.

Addressing goes through the SDK's own mail helper rather than
email.FormatAddress, because the payload carries the display name and the
address as separate JSON fields, with nothing for a comma to split.
*/
package sendgrid
