package requestsigning_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v10/cryptography/requestsigning"
)

// The receiving end. Verification is where these schemes are actually got
// wrong, so it ships with the sender rather than being described in prose for
// each receiver to reimplement.
func ExampleVerify() {
	keyring := requestsigning.Keyring{Current: []byte("the shared signing key")}

	receiver := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		// The exact bytes received, read before any decoding. Decoding and
		// re-encoding changes key order and whitespace, and the signature
		// covers bytes rather than meaning.
		body, err := io.ReadAll(req.Body)
		if err != nil {
			res.WriteHeader(http.StatusBadRequest)

			return
		}

		signature := req.Header.Get(requestsigning.SignatureHeader)
		if err = requestsigning.Verify(keyring, body, signature); err != nil {
			res.WriteHeader(http.StatusUnauthorized)

			return
		}

		res.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	payload := []byte(`{"id":"order-7"}`)

	signature, err := requestsigning.Sign(keyring, payload, time.Now())
	if err != nil {
		panic(err)
	}

	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, receiver.URL, strings.NewReader(string(payload)))
	if err != nil {
		panic(err)
	}

	req.Header.Set(requestsigning.SignatureHeader, signature)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer func() { _ = res.Body.Close() }()

	fmt.Println(res.StatusCode)

	// A tampered body no longer verifies.
	fmt.Println(requestsigning.Verify(keyring, []byte(`{"id":"order-8"}`), signature))

	// Output:
	// 204
	// invalid request signature
}

// Rotation is why Keyring is a pair. A request is signed under both keys while
// Previous is set, so either side can switch without coordinating an instant of
// downtime with the other.
func ExampleSign() {
	rotating := requestsigning.Keyring{
		Current:  []byte("the new key"),
		Previous: []byte("the outgoing key"),
	}

	payload := []byte(`{"id":"order-7"}`)
	signedAt := time.Unix(1753900000, 0)

	signature, err := requestsigning.Sign(rotating, payload, signedAt)
	if err != nil {
		panic(err)
	}

	// Two s= components: a receiver that has moved to the new key and one that
	// has not both find a signature they can verify.
	fmt.Println(strings.Count(signature, ",s="))

	fmt.Println(requestsigning.Verify(
		requestsigning.Keyring{Current: []byte("the new key")},
		payload, signature, requestsigning.WithVerificationTime(signedAt),
	))
	fmt.Println(requestsigning.Verify(
		requestsigning.Keyring{Current: []byte("the outgoing key")},
		payload, signature, requestsigning.WithVerificationTime(signedAt),
	))

	// Output:
	// 2
	// <nil>
	// <nil>
}

// A Signer and a Verifier built from one key source are the two halves a
// service wires: the client stamps, the server checks, and neither holds key
// material of its own.
func ExampleNewSigner() {
	keys := requestsigning.StaticKeyring(requestsigning.Keyring{Current: []byte("the shared key")})

	// In a real service these come from secrets, via NewSecretKeySource, so a
	// rotation is a change in the store rather than a deploy.
	signer, err := requestsigning.NewSigner(keys)
	if err != nil {
		panic(err)
	}

	verifier, err := requestsigning.NewVerifier(keys)
	if err != nil {
		panic(err)
	}

	body := []byte(`{"amount":4200}`)

	header := http.Header{}
	if err = signer.SignHeaders(context.Background(), header, body); err != nil {
		panic(err)
	}

	// The verifier names the header it reads, so a caller wiring one does not
	// have to know which scheme it got.
	fmt.Println(verifier.VerifyHeaderValue(context.Background(), header.Get(verifier.HeaderName()), body))

	// An unsigned request is refused the same way a badly signed one is.
	fmt.Println(verifier.VerifyHeaderValue(context.Background(), "", body))

	// Output:
	// <nil>
	// no X-Platform-Signature header: invalid request signature
}
