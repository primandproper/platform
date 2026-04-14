/*
Package authentication defines interfaces for password hashing and credential
verification. Second-factor verification (TOTP, WebAuthn, etc.) lives in
sibling packages (e.g. authentication/totp) so callers can compose whichever
factors their application needs.
*/
package authentication
