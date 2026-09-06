// Package mobilecfg selects and builds a mobile.PushSender from configuration:
// APNs for iOS, FCM for Android, apns_fcm for both, or noop.
//
// The provider names which platforms are on, and nothing else does. Presence of
// a sub-config decides nothing, which is what lets an empty FCM block mean "use
// Application Default Credentials" rather than "Android is off" — and what makes
// the provider, not the credentials, the thing to read when a platform stops
// receiving pushes.
//
// The sub-configs are the leaf packages' own structs rather than parallel copies
// of them, so a field added to apns.Config is configurable the moment it exists.
//
// # Why the token invalidator is resolved under its own interface
//
// A provider answering a push with "this token is dead" raises
// mobile.ErrTokenInvalid, and a sender holding an invalidator deletes the row
// rather than addressing the same handset again tomorrow.
// notificationscfg.RegisterStore registers a store that does the deleting, and
// RegisterPushSender here picks it up — so a container carrying both halves
// prunes, and one carrying only the sender behaves exactly as it did before.
//
// What that registration resolves is mobile.TokenInvalidator, and it used to be
// notifications.Registry. The two are satisfied by the same value, so the
// behavior is unchanged; what changed is which package this one names.
//
// The reason is the module's tier split, and this is the mirror of the other
// three cuts it forced. In authorizationcfg, webauthncfg and oauth2servercfg a
// primitive's config reached into a domain package to *build* a store, and the
// answer was to move the provider string to the half that owns the table. Here a
// primitive's config reached into a domain package to *name a key*, for one
// method it could already spell: an invalidator is two strings and an error,
// while a registry is five methods over database.Tx, tenancy.Scope and a row
// type. Naming the wider one made every deployment that sends a push depend on
// the package that owns the device table, to reach a method the narrow interface
// already declares.
//
// So there was nothing to split — the import was never load-bearing.
// notifications.Registry's own documentation already said the plain-string
// InvalidateDeviceToken exists so that a registry is wirable into a sender
// without either package importing the other; narrowing the key is what finishes
// that sentence, and the narrowing itself is registered on the domain side,
// where naming a primitive is the direction the split permits.
//
// The consequence for a wiring site is nothing at all: notificationscfg registers
// its store under this key alongside the three it already used, so a container
// that wired both halves before wires both halves now.
package mobilecfg
