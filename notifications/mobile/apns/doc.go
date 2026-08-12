/*
Package apns sends push notifications to iOS devices through Apple's APNs.

Choosing it commits a deployment to the whole of an Apple credential set, and
every part of it is required: the .p8 signing key as a file this process can
read, the key's ID, the Apple Developer team ID, and the app's bundle ID, which
doubles as the APNs topic. There is no ambient-credential path — nothing here
corresponds to the fcm sibling's Application Default Credentials, where an
entirely empty config is valid.

The key is read and a token minted during construction, so an unreadable path or
a malformed key fails at startup rather than on the first send.

# Sandbox and production are different gateways

Config.Production selects Apple's production host; false selects the
development one. A device token minted against one environment is rejected by
the other, which is the usual cause of a deployment that sends cleanly in
staging and gets every notification refused in production. There is no
autodetection: the flag is the whole of the decision.

# What a notification can carry

Send takes a title, a body, and an optional badge count, and sends at high
priority. Sound, custom data payloads, collapse IDs, thread IDs, mutable or
content-available flags, and expiry are not exposed. A caller needing any of
them needs a different path to APNs.

Device tokens are checked locally first — 64 hexadecimal characters, either case
— so a malformed token fails without a network call.

# Failures

APNs answers a rejected notification with a 200-level exchange and a reason,
which is not an error to the HTTP client. Send treats it as one: an unsuccessful
response returns an error carrying the reason and status code, and puts both on
the span alongside the APNs message ID. A transport failure — APNs unreachable
rather than unhappy — is reported the same way. Nothing here retries, and there
is no circuit breaker on this path.
*/
package apns
