/*
Package dataprivacycfg assembles the data privacy machinery from environment
configuration: the Store every part shares, the Service applications submit
through, the Worker that fulfills, and the Sweeper that expires.

All four read one Config, so the dialect and table prefix the Service writes to
are by construction the ones the Worker claims from and the Sweeper expires.

The registry is not configured here. Which domains hold data about a person is
Go code — a set of interface implementations — and there is no useful way to
express it in the environment. It is passed explicitly to NewWorker.
*/
package dataprivacycfg

import (
	"context"

	"github.com/primandproper/platform-go/v9/audit"
	"github.com/primandproper/platform-go/v9/compression"
	"github.com/primandproper/platform-go/v9/cryptography/encryption"
	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/database/dialect"
	"github.com/primandproper/platform-go/v9/dataprivacy"
	"github.com/primandproper/platform-go/v9/dataprivacy/auditerasure"
	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/uploads"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config assembles a dataprivacy Store, Service, Worker, and Sweeper.
type Config struct {
	_ struct{} `json:"-" yaml:"-"`

	// Dialect selects the SQL emitted; it must match the database.Client.
	Dialect dialect.Dialect `env:"DIALECT" json:"dialect" yaml:"dialect"`

	// TablePrefix names the request table. It must match the prefix the
	// migrations were rendered with. Defaults to
	// dataprivacy.DefaultTablePrefix.
	TablePrefix string `env:"TABLE_PREFIX" json:"tablePrefix" yaml:"tablePrefix"`

	// AuditErasure configures the audit log's own eraser.
	AuditErasure AuditErasureConfig `env:",init" envPrefix:"AUDIT_ERASURE_" json:"auditErasure" yaml:"auditErasure"`

	// Worker carries the fulfillment loop's knobs.
	Worker dataprivacy.WorkerConfig `env:",init" envPrefix:"WORKER_" json:"worker" yaml:"worker"`

	// Service carries the request state machine's timings.
	Service dataprivacy.ServiceConfig `env:",init" envPrefix:"SERVICE_" json:"service" yaml:"service"`

	// Sweeper carries the expiry and retention knobs.
	Sweeper dataprivacy.SweeperConfig `env:",init" envPrefix:"SWEEPER_" json:"sweeper" yaml:"sweeper"`
}

// AuditErasureConfig configures whether, and how, an erasure touches the audit
// log.
//
// It is a config section rather than a plain registration because "do we erase
// our own audit records about this person" is a policy question with a
// different answer per jurisdiction and per deployment, and it should be
// answerable by an operator without a code change.
type AuditErasureConfig struct {
	// TablePrefix is the prefix the audit tables carry. Defaults to
	// audit.DefaultTablePrefix, and must match the audit Recorder's.
	TablePrefix string `env:"TABLE_PREFIX" json:"tablePrefix" yaml:"tablePrefix"`

	// RetentionBasis is the wording recorded against audit entries that cannot
	// be removed. Defaults to auditerasure.DefaultRetentionBasis.
	RetentionBasis string `env:"RETENTION_BASIS" json:"retentionBasis" yaml:"retentionBasis"`

	// Disabled stops the audit eraser being registered, leaving the audit log
	// entirely untouched by an erasure.
	//
	// The polarity is deliberate: an erasure that silently skipped a store of
	// personal data would be the more surprising default, so the eraser is on
	// unless an operator turns it off. Turning it off is the right call where
	// retention of audit records is mandatory, and the whole reason this is a
	// configuration flag rather than a code change.
	//
	// Note what "on" does and does not mean. The eraser deletes whole audit
	// scopes belonging to the subject and reports everything else as retained —
	// it never deletes entries from the middle of a chain, because that would
	// make audit.Reader.Verify report tampering for the rest of that scope's
	// history. See the dataprivacy/auditerasure package documentation.
	Disabled bool `env:"DISABLED" json:"disabled" yaml:"disabled"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults fills in zero fields.
func (cfg *Config) EnsureDefaults() {
	if cfg.TablePrefix == "" {
		cfg.TablePrefix = dataprivacy.DefaultTablePrefix
	}

	if cfg.AuditErasure.TablePrefix == "" {
		cfg.AuditErasure.TablePrefix = audit.DefaultTablePrefix
	}

	if cfg.AuditErasure.RetentionBasis == "" {
		cfg.AuditErasure.RetentionBasis = auditerasure.DefaultRetentionBasis
	}

	cfg.Service.EnsureDefaults()
	cfg.Worker.EnsureDefaults()
	cfg.Sweeper.EnsureDefaults()
}

// ValidateWithContext validates a Config.
//
// The nested configs are validated through validation.By closures because ozzo
// dereferences a struct-value field before checking ValidatableWithContext, so
// they would otherwise be skipped.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Dialect, validation.Required, validation.By(func(any) error {
			if !cfg.Dialect.Valid() {
				return errors.Wrapf(dialect.ErrUnsupported, "dataprivacy dialect %q", cfg.Dialect)
			}

			return nil
		})),
		validation.Field(&cfg.Service, validation.By(func(any) error {
			return cfg.Service.ValidateWithContext(ctx)
		})),
		validation.Field(&cfg.Worker, validation.By(func(any) error {
			return cfg.Worker.ValidateWithContext(ctx)
		})),
		validation.Field(&cfg.Sweeper, validation.By(func(any) error {
			return cfg.Sweeper.ValidateWithContext(ctx)
		})),
	)
}

// prepare fills defaults and validates, which every constructor below does
// first and identically.
func (cfg *Config) prepare(ctx context.Context) error {
	if cfg == nil {
		return errors.ErrNilInputParameter
	}

	cfg.EnsureDefaults()

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return errors.Wrap(err, "validating dataprivacy config")
	}

	return nil
}

// NewStore builds the Store every part shares. client must be the database
// holding the request table.
//
// Explicit options run after the config-derived ones, so a caller can still
// override anything.
func NewStore(
	ctx context.Context,
	cfg *Config,
	client database.Client,
	opts ...Option,
) (dataprivacy.Store, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	if err := cfg.prepare(ctx); err != nil {
		return nil, err
	}

	base := []dataprivacy.SQLStoreOption{dataprivacy.WithTablePrefix(cfg.TablePrefix)}

	if logger != nil {
		base = append(base, dataprivacy.WithStoreLogger(logger))
	}
	if tracerProvider != nil {
		base = append(base, dataprivacy.WithStoreTracerProvider(tracerProvider))
	}
	if metricsProvider != nil {
		base = append(base, dataprivacy.WithStoreMetricsProvider(metricsProvider))
	}

	return dataprivacy.NewSQLStore(client, append(base, o.store...)...)
}

// NewService builds the Service applications submit through.
//
// Explicit options run after the config-derived ones, so a caller can still
// override anything.
func NewService(
	ctx context.Context,
	cfg *Config,
	store dataprivacy.Store,
	opts ...Option,
) (dataprivacy.Service, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	if err := cfg.prepare(ctx); err != nil {
		return nil, err
	}

	var base []dataprivacy.ServiceOption
	if logger != nil {
		base = append(base, dataprivacy.WithServiceLogger(logger))
	}
	if tracerProvider != nil {
		base = append(base, dataprivacy.WithServiceTracerProvider(tracerProvider))
	}
	if metricsProvider != nil {
		base = append(base, dataprivacy.WithServiceMetricsProvider(metricsProvider))
	}

	return dataprivacy.NewService(ctx, &cfg.Service, store, append(base, o.service...)...)
}

// NewWorker builds the Worker that fulfills requests.
//
// The registry is a required argument rather than a config field: which domains
// hold data about a person is Go code. A worker with an empty registry is
// refused by dataprivacy.NewWorker, which is the correct failure but a
// confusing one to debug, so it is passed explicitly here.
//
// uploader may be nil for an erasure-only worker. encrypted must say whether
// artifacts are written encrypted — it decides whether a notification can carry
// a download link at all, and it is a bool rather than the encryptor itself
// because this constructor never encrypts anything, it only needs to know.
// Pass the encryptor through EnsurePackaging.
func NewWorker(
	ctx context.Context,
	cfg *Config,
	store dataprivacy.Store,
	registry *dataprivacy.Registry,
	uploader uploads.UploadManager,
	encrypted bool,
	opts ...Option,
) (*dataprivacy.Worker, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	if err := cfg.prepare(ctx); err != nil {
		return nil, err
	}

	var base []dataprivacy.WorkerOption
	if uploader != nil {
		// Wired here so a completion notification carries a working link
		// without the caller assembling the signer by hand — and so its TTL is
		// by construction the one Service.Download would have used.
		base = append(base,
			dataprivacy.WithWorkerUploadManager(uploader),
			dataprivacy.WithWorkerURLSigner(dataprivacy.NewArtifactURLSigner(
				uploader, cfg.Service.SignedURLTTL, encrypted,
			)),
		)
	}

	if logger != nil {
		base = append(base, dataprivacy.WithWorkerLogger(logger))
	}
	if tracerProvider != nil {
		base = append(base, dataprivacy.WithWorkerTracerProvider(tracerProvider))
	}
	if metricsProvider != nil {
		base = append(base, dataprivacy.WithWorkerMetricsProvider(metricsProvider))
	}

	return dataprivacy.NewWorker(ctx, &cfg.Worker, store, registry, append(base, o.worker...)...)
}

// NewSweeper builds the Sweeper. Register its Job with a jobs.Scheduler; see
// dataprivacy.Sweeper.Job.
func NewSweeper(
	ctx context.Context,
	cfg *Config,
	store dataprivacy.Store,
	uploader uploads.UploadManager,
	opts ...Option,
) (*dataprivacy.Sweeper, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	if err := cfg.prepare(ctx); err != nil {
		return nil, err
	}

	base := []dataprivacy.SweeperOption{dataprivacy.WithSweeperUploadManager(uploader)}
	if logger != nil {
		base = append(base, dataprivacy.WithSweeperLogger(logger))
	}
	if tracerProvider != nil {
		base = append(base, dataprivacy.WithSweeperTracerProvider(tracerProvider))
	}
	if metricsProvider != nil {
		base = append(base, dataprivacy.WithSweeperMetricsProvider(metricsProvider))
	}

	return dataprivacy.NewSweeper(ctx, &cfg.Sweeper, store, append(base, o.sweeper...)...)
}

// RegisterAuditEraser registers the audit log's eraser into registry unless
// AuditErasure.Disabled is set.
//
// It returns whether it registered, so a caller can log which way the policy
// went. "Did this deployment erase audit records" is a question that gets asked
// long after the deployment, and a boolean in a config file somewhere is a poor
// place to have recorded the answer.
func RegisterAuditEraser(
	ctx context.Context,
	cfg *Config,
	registry *dataprivacy.Registry,
	opts ...auditerasure.Option,
) (bool, error) {
	if err := cfg.prepare(ctx); err != nil {
		return false, err
	}

	if cfg.AuditErasure.Disabled {
		return false, nil
	}

	if registry == nil {
		return false, errors.Wrap(errors.ErrNilInputParameter, "nil dataprivacy registry")
	}

	base := []auditerasure.Option{auditerasure.WithRetentionBasis(cfg.AuditErasure.RetentionBasis)}

	eraser, err := auditerasure.New(cfg.Dialect, cfg.AuditErasure.TablePrefix, append(base, opts...)...)
	if err != nil {
		return false, errors.Wrap(err, "building dataprivacy audit eraser")
	}

	if err = registry.RegisterEraser(auditerasure.DefaultKey, eraser); err != nil {
		return false, errors.Wrap(err, "registering dataprivacy audit eraser")
	}

	return true, nil
}

// EnsurePackaging returns the compressor and encryptor pair the Worker writes
// artifacts with and the Service reads them with.
//
// It exists so the two cannot be configured apart. An artifact written with one
// compressor and read with another is unreadable, and the failure surfaces at
// the subject rather than at startup.
func EnsurePackaging(
	compressor compression.Compressor,
	encryptorDecryptor encryption.EncryptorDecryptor,
) (workerOpts []dataprivacy.WorkerOption, serviceOpts []dataprivacy.ServiceOption) {
	if compressor != nil {
		workerOpts = append(workerOpts, dataprivacy.WithWorkerCompressor(compressor))
		serviceOpts = append(serviceOpts, dataprivacy.WithServiceCompressor(compressor))
	}

	if encryptorDecryptor != nil {
		workerOpts = append(workerOpts, dataprivacy.WithWorkerEncryptor(encryptorDecryptor))
		serviceOpts = append(serviceOpts, dataprivacy.WithServiceDecryptor(encryptorDecryptor))
	}

	return workerOpts, serviceOpts
}
