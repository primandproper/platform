/*
Package email sends transactional mail, over one of six vendors or nowhere at
all.

The interface is Emailer, and it is deliberately one method: send this
OutboundEmailMessage, or say why not. Templating, retries beyond the vendor's
own, batching, and scheduling are not here — an application that wants them
composes them around this, and an application that does not is spared them.

# The providers

Each is a subpackage translating an OutboundEmailMessage onto one vendor's API,
and each constructor returns its own concrete type rather than the interface:

	mailgun    Mailgun's messages API
	mailjet    Mailjet's v3.1 send API
	postmark   Postmark's email API
	resend     Resend's emails API
	sendgrid   SendGrid's v3 mail send
	ses        Amazon SES
	noop       accepts every message and delivers none

Six vendors and a noop, because near-identical translations to seven different
APIs are seven files a reader opens for the lines that differ. They are left
that way deliberately; see llm/doc.go for the long form of that decision.

[github.com/primandproper/platform-go/v14/email/config] is what selects one from
configuration and wraps it in a circuit breaker. Its provider roster is the
ground truth for the list above, which is checked against it rather than
maintained beside it.

noop is not a fallback. An unset or misspelled provider is
[github.com/primandproper/platform-go/v14/errors.ErrUnknownProvider], because
outbound mail that silently goes nowhere is discovered by the people who never
received it. Discarding mail has to be asked for by name.

# Addresses

FormatAddress is the one place this module escapes a display name into an
RFC 5322 mailbox, and every provider whose payload carries a mailbox header —
mailgun, postmark, resend, ses — reaches it. A second copy of that escaping is a
second chance to get a comma wrong, and nothing would say which copy did.
SendGrid and Mailjet carry the name and the address as separate JSON fields and
so escape nothing; each of their docs says why.
*/
package email
