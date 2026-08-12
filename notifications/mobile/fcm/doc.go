/*
Package fcm sends push notifications to Android devices through Firebase Cloud
Messaging.

An entirely empty Config is a valid one: it asks for Application Default
Credentials, which is how a service running on GCP normally authenticates.
Config.CredentialsPath names a Firebase service account JSON file instead, for
everywhere else. Selecting this provider is what turns Android push on — the
config validates nothing, because there is nothing it could require that would
not break the ADC case.

That is the sharpest contrast with the apns sibling, which has no ambient
credential path at all and requires four values before it will build.

# What a notification can carry

Send takes a device token, a title, and a body, and sends a notification-only
message. There is no data payload, no Android-specific block — priority,
collapse key, TTL, channel ID, icon, click action — and no topic or condition
fan-out: this is one message to one token.

FCM has no badge concept, so mobile.MultiPlatformPushSender logs and drops a
BadgeCount when it routes to this path rather than pretending it was delivered.
A caller that needs a badge on Android has to carry it in application state.

# Failures

The FCM message ID is recorded on the span for a successful send. A failure —
including an unregistered or invalid token — comes back as the SDK's error,
wrapped. Nothing here retries, and there is no circuit breaker on this path.
*/
package fcm
