package grpc

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
//
// Port is not Required, because zero is meaningful: it asks the OS for an
// ephemeral port, the same reading server/http gives it.
//
// The TLS pair is checked together because NewGRPCServer enables TLS only when
// both files are named. Supplying one of them looks like TLS was configured and
// serves plaintext, which is the failure worth refusing at startup.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	bothOrNeither := func(other string) validation.Rule {
		return validation.When(other != "", validation.Required)
	}

	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.TLSCertificateFile, bothOrNeither(cfg.TLSCertificateKeyFile)),
		validation.Field(&cfg.TLSCertificateKeyFile, bothOrNeither(cfg.TLSCertificateFile)),
	)
}
