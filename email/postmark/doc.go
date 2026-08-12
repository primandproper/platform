/*
Package postmark sends email through Postmark.

Choosing it commits a deployment to a Postmark server token, which is
per-server rather than per-account and therefore selects which stream the mail
is billed and reported against. Config.BaseURL overrides the API host, which is
what points the package at an httptest server.

# The context does not reach the wire

This is the one emailer here whose vendor client takes no context. SendEmail
accepts one, and uses it for the span and for the metrics it records, but the
HTTP call underneath is issued without it: cancelling the context or letting its
deadline lapse does not abort a send in flight. What bounds a send is the
timeout on the *http.Client the caller supplied. Every other provider in this
family passes the context through to the request.

# What one send is

SendEmail delivers one HTML message to one recipient and returns when Postmark
has answered, recording the returned message ID on the span. It is synchronous
and does not retry; a failed send scores the circuit breaker, and while that
breaker is open every call returns circuitbreaking.ErrCircuitBroken. Addresses
are rendered through email.FormatAddress, which quotes the display name so a
comma in it cannot inject recipients.
*/
package postmark
