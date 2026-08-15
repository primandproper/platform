package webauthn

import (
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
)

// The protocol types this package works in, aliased rather than wrapped.
//
// They are aliases — not definitions — so a value crossing this seam is the
// library's own type and needs no conversion: a [Credential] handed back by
// FinishRegistration is exactly what the library produced, and a [User] the
// application already implements for go-webauthn satisfies this one without
// being told about this package.
//
// Wrapping them was the alternative, and it buys nothing here. These are wire
// types fixed by the WebAuthn specification rather than by the library, so
// there is no vendor detail to hide, and a wrapper would have to be converted
// at every boundary — including inside a DiscoverableUserHandler, which the
// library calls and which would then be handing back the wrong type.
//
// What the aliases do buy is that the common path imports one package: a
// consumer registering and verifying passkeys never imports go-webauthn
// directly unless it reaches for the per-ceremony options, which are the
// library's and are passed through as such.
type (
	// User is the Relying Party's user account, as the ceremonies need it: a
	// handle, the two display strings, and the credentials the account owns.
	//
	// Implementing it is the application's job and is usually an adapter of
	// twenty lines over whatever the application already stores. WebAuthnID is
	// the one with a rule worth repeating — it is an opaque handle of at most
	// 64 bytes, and every authentication decision is made against it rather
	// than against the name.
	User = gowebauthn.User

	// SessionData is the ceremony state that has to outlive the request that
	// issued it: the challenge, the user handle, the allowed credentials, and
	// the deadline. It is what a [SessionStore] stores.
	SessionData = gowebauthn.SessionData

	// Credential is a registered passkey — the credential ID, the public key,
	// and the authenticator's sign count. FinishRegistration returns one to
	// persist; FinishLogin returns the one that was used, whose sign count is
	// worth writing back.
	Credential = gowebauthn.Credential

	// DiscoverableUserHandler resolves the user behind a credential during a
	// discoverable (usernameless) login, given the raw credential ID and the
	// user handle the authenticator returned.
	DiscoverableUserHandler = gowebauthn.DiscoverableUserHandler

	// RegistrationOption adjusts one registration ceremony — the authenticator
	// selection, the exclusion list, the attestation conveyance. It is
	// per-ceremony rather than per-Relying-Party, which is why it is passed to
	// BeginRegistration rather than configured.
	RegistrationOption = gowebauthn.RegistrationOption

	// LoginOption adjusts one login ceremony — the allowed credentials, the
	// user verification requirement, the extensions.
	LoginOption = gowebauthn.LoginOption
)
