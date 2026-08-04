package grpc

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("accepts a plaintext server", func(t *testing.T) {
		t.Parallel()

		must.NoError(t, (&Config{}).ValidateWithContext(t.Context()))
	})

	T.Run("accepts a complete TLS pair", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{TLSCertificateFile: "cert.pem", TLSCertificateKeyFile: "key.pem"}

		must.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("refuses a certificate with no key", func(t *testing.T) {
		t.Parallel()

		// Half a pair reads as "TLS is configured" and serves plaintext.
		cfg := &Config{TLSCertificateFile: "cert.pem"}

		err := cfg.ValidateWithContext(t.Context())
		must.Error(t, err)
		test.StrContains(t, err.Error(), "tlsCertificateKey")
	})

	T.Run("refuses a key with no certificate", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{TLSCertificateKeyFile: "key.pem"}

		err := cfg.ValidateWithContext(t.Context())
		must.Error(t, err)
		test.StrContains(t, err.Error(), "tlsCertificate")
	})
}
