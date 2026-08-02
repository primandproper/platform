package http

import (
	"context"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type (
	// Config describes the settings pertinent to the HTTP serving portion of the service.
	Config struct {
		_ struct{} `json:"-" yaml:"-"`

		// AppleAppSiteAssociation, when populated, causes the server to serve the
		// apple-app-site-association file at AppleAppSiteAssociationPath.
		AppleAppSiteAssociation *AppleAppSiteAssociationConfig `env:",init" envPrefix:"APPLE_APP_SITE_ASSOCIATION_" json:"appleAppSiteAssociation,omitempty" yaml:"appleAppSiteAssociation,omitempty"`

		SSLCertificateFile    string        `env:"SSL_CERTIFICATE_FILEPATH"     json:"sslCertificate,omitempty"    yaml:"sslCertificate,omitempty"`
		SSLCertificateKeyFile string        `env:"SSL_CERTIFICATE_KEY_FILEPATH" json:"sslCertificateKey,omitempty" yaml:"sslCertificateKey,omitempty"`
		StartupDeadline       time.Duration `env:"STARTUP_DEADLINE"             json:"startupDeadline,omitempty"   yaml:"startupDeadline,omitempty"`
		ReadTimeout           time.Duration `env:"READ_TIMEOUT"                 json:"readTimeout,omitempty"       yaml:"readTimeout,omitempty"`
		WriteTimeout          time.Duration `env:"WRITE_TIMEOUT"                json:"writeTimeout,omitempty"      yaml:"writeTimeout,omitempty"`
		IdleTimeout           time.Duration `env:"IDLE_TIMEOUT"                 json:"idleTimeout,omitempty"       yaml:"idleTimeout,omitempty"`
		Port                  uint16        `env:"PORT"                         json:"port"                        yaml:"port"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
//
// Neither Port nor StartupDeadline is Required, because zero is meaningful for
// both: port 0 asks the OS for an ephemeral port, and a zero StartupDeadline
// means the bind is unbounded. They were Required while nothing called this,
// which is how the rules came to reject configurations the server accepts.
//
// The timeouts are checked for sign instead: a negative one is always a
// mistake, and net/http reads it as "no timeout" rather than rejecting it.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(
		ctx,
		cfg,
		validation.Field(&cfg.StartupDeadline, validation.Min(time.Duration(0))),
		validation.Field(&cfg.ReadTimeout, validation.Min(time.Duration(0))),
		validation.Field(&cfg.WriteTimeout, validation.Min(time.Duration(0))),
		validation.Field(&cfg.IdleTimeout, validation.Min(time.Duration(0))),
		validation.Field(&cfg.AppleAppSiteAssociation),
	)
}
