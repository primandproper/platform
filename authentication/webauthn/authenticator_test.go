package webauthn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/shoenig/test/must"
)

// A virtual authenticator, because the alternative is not testing the
// ceremonies.
//
// Everything this package does sits either side of a signature made by a
// security key: the ceremony is begun, a device signs the challenge, and the
// ceremony is finished by verifying that signature. Stubbing the library out
// would leave the halves this package actually owns — the challenge going into
// the store and coming back out exactly once — asserted against nothing, since
// they are only observable through a ceremony that completes.
//
// So this is a real ES256 authenticator: a P-256 key, a COSE public key, an
// authenticator data structure, and a signature over the bytes the
// specification says to sign. It produces the two response payloads a browser
// would POST. What it does not do is attestation — it registers with the "none"
// format, which is what a passkey deployment asks for anyway.
const (
	// Authenticator data flags, from the specification. The two this
	// authenticator does not set — backup eligible and backup state — are
	// deliberately absent in both ceremonies: a credential registered with one
	// answer and asserted with the other is refused by the library for an
	// inconsistency that is real, and would look here like a flaky test.
	flagUserPresent            = 0x01
	flagUserVerified           = 0x04
	flagAttestedCredentialData = 0x40

	// coseKeyType, coseAlgorithm, coseCurve are the COSE labels for an ES256
	// key on P-256: kty=EC2(2), alg=ES256(-7), crv=P-256(1).
	coseKeyType   = 2
	coseAlgorithm = -7
	coseCurve     = 1

	// aaguidLength is fixed by the attested credential data layout: sixteen
	// zero bytes here, which is what an authenticator that declines to identify
	// its model reports.
	aaguidLength = 16

	// coordinateLength is the width of one P-256 coordinate.
	coordinateLength = 32
)

// virtualAuthenticator is one passkey on one device.
type virtualAuthenticator struct {
	key          *ecdsa.PrivateKey
	rpID         string
	origin       string
	credentialID []byte
	signCount    uint32
}

// newAuthenticator mints a device with one credential.
func newAuthenticator(tb testing.TB, rpID, origin string) *virtualAuthenticator {
	tb.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must.NoError(tb, err)

	credentialID := make([]byte, 32)
	_, err = rand.Read(credentialID)
	must.NoError(tb, err)

	return &virtualAuthenticator{
		key:          key,
		credentialID: credentialID,
		rpID:         rpID,
		origin:       origin,
		signCount:    1,
	}
}

// register produces the attestation response a browser would POST to finish a
// registration ceremony for challenge.
func (a *virtualAuthenticator) register(tb testing.TB, challenge string) []byte {
	tb.Helper()

	clientData := a.clientData(tb, "webauthn.create", challenge)
	authData := a.authenticatorData(tb, flagUserPresent|flagUserVerified|flagAttestedCredentialData, a.attestedCredentialData(tb))

	attestation, err := cbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData,
	})
	must.NoError(tb, err)

	return marshalResponse(tb, a.credentialID, map[string]string{
		"clientDataJSON":    encode(clientData),
		"attestationObject": encode(attestation),
	})
}

// assert produces the assertion response a browser would POST to finish a login
// ceremony for challenge. userHandle is echoed back by the authenticator, and is
// what a discoverable login identifies the user by.
func (a *virtualAuthenticator) assert(tb testing.TB, challenge string, userHandle []byte) []byte {
	tb.Helper()

	a.signCount++

	clientData := a.clientData(tb, "webauthn.get", challenge)
	authData := a.authenticatorData(tb, flagUserPresent|flagUserVerified, nil)

	// The signature covers the authenticator data followed by the hash of the
	// client data, which is what ties one signature to one challenge from one
	// origin.
	clientDataHash := sha256.Sum256(clientData)
	signed := sha256.Sum256(append(append([]byte{}, authData...), clientDataHash[:]...))

	signature, err := ecdsa.SignASN1(rand.Reader, a.key, signed[:])
	must.NoError(tb, err)

	response := map[string]string{
		"clientDataJSON":    encode(clientData),
		"authenticatorData": encode(authData),
		"signature":         encode(signature),
	}

	if len(userHandle) > 0 {
		response["userHandle"] = encode(userHandle)
	}

	return marshalResponse(tb, a.credentialID, response)
}

// credential is the registered passkey as this package's callers store it, for
// the tests that need a user who already has one.
func (a *virtualAuthenticator) credential(tb testing.TB) Credential {
	tb.Helper()

	return Credential{
		ID:              a.credentialID,
		PublicKey:       a.coseKey(tb),
		AttestationType: "none",
		Flags: gowebauthn.CredentialFlags{
			UserPresent:  true,
			UserVerified: true,
		},
		Authenticator: gowebauthn.Authenticator{SignCount: a.signCount},
	}
}

// clientData renders the collected client data for one ceremony step.
func (a *virtualAuthenticator) clientData(tb testing.TB, ceremony, challenge string) []byte {
	tb.Helper()

	data, err := json.Marshal(map[string]any{
		"type":        ceremony,
		"challenge":   challenge,
		"origin":      a.origin,
		"crossOrigin": false,
	})
	must.NoError(tb, err)

	return data
}

// authenticatorData renders the authenticator data structure: the relying
// party's hash, the flags, the counter, and — for a registration — the
// credential itself.
func (a *virtualAuthenticator) authenticatorData(tb testing.TB, flags byte, attested []byte) []byte {
	tb.Helper()

	rpIDHash := sha256.Sum256([]byte(a.rpID))

	data := make([]byte, 0, sha256.Size+1+4+len(attested))
	data = append(data, rpIDHash[:]...)
	data = append(data, flags)
	data = binary.BigEndian.AppendUint32(data, a.signCount)

	return append(data, attested...)
}

// attestedCredentialData renders the credential a registration announces: the
// authenticator's AAGUID, the credential ID, and the public key.
func (a *virtualAuthenticator) attestedCredentialData(tb testing.TB) []byte {
	tb.Helper()

	data := make([]byte, aaguidLength)
	data = binary.BigEndian.AppendUint16(data, uint16(len(a.credentialID)))

	data = append(data, a.credentialID...)

	return append(data, a.coseKey(tb)...)
}

// coseKey renders the public key in the COSE encoding the specification
// requires, deterministically so that the same key renders the same bytes.
func (a *virtualAuthenticator) coseKey(tb testing.TB) []byte {
	tb.Helper()

	encoder, encErr := cbor.CanonicalEncOptions().EncMode()
	must.NoError(tb, encErr)

	// The uncompressed point is a leading 0x04 followed by the two
	// coordinates, which is where the COSE encoding's x and y come from.
	point, err := a.key.PublicKey.Bytes()
	must.NoError(tb, err)
	must.SliceLen(tb, 1+2*coordinateLength, point)

	key, err := encoder.Marshal(map[int]any{
		1:  coseKeyType,
		3:  coseAlgorithm,
		-1: coseCurve,
		-2: point[1 : 1+coordinateLength],
		-3: point[1+coordinateLength:],
	})
	must.NoError(tb, err)

	return key
}

// marshalResponse wraps one ceremony's response in the credential envelope the
// browser's WebAuthn API produces.
func marshalResponse(tb testing.TB, credentialID []byte, response map[string]string) []byte {
	tb.Helper()

	body, err := json.Marshal(map[string]any{
		"id":       encode(credentialID),
		"rawId":    encode(credentialID),
		"type":     "public-key",
		"response": response,
	})
	must.NoError(tb, err)

	return body
}

// encode renders bytes the way every field of a WebAuthn response is rendered.
func encode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
