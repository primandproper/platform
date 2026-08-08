package dataprivacy

import (
	"context"
	"time"

	"github.com/primandproper/platform-go/v10/audit"
	"github.com/primandproper/platform-go/v10/clock"
	"github.com/primandproper/platform-go/v10/compression"
	"github.com/primandproper/platform-go/v10/cryptography/encryption"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	"github.com/primandproper/platform-go/v10/observability/tracing"
	"github.com/primandproper/platform-go/v10/uploads"
)

// EmailNotifierOption configures an EmailNotifier.
type EmailNotifierOption func(*EmailNotifier)

// WithMessageRenderer replaces the default message.
func WithMessageRenderer(renderer MessageRenderer) EmailNotifierOption {
	return func(n *EmailNotifier) {
		if renderer != nil {
			n.renderer = renderer
		}
	}
}

// ServiceOption configures a Service.
type ServiceOption func(*service)

// WithServiceClock swaps the clock stamping submission, deadline, and expiry.
func WithServiceClock(c clock.Clock) ServiceOption {
	return func(s *service) {
		if c != nil {
			s.clock = c
		}
	}
}

// WithServiceLogger attaches a logger.
func WithServiceLogger(logger logging.Logger) ServiceOption {
	return func(s *service) {
		s.logger = logger
	}
}

// WithServiceTracerProvider attaches a tracer provider.
func WithServiceTracerProvider(tracerProvider tracing.TracerProvider) ServiceOption {
	return func(s *service) {
		s.tracerProvider = tracerProvider
	}
}

// WithServiceMetricsProvider attaches a metrics provider.
func WithServiceMetricsProvider(metricsProvider metrics.Provider) ServiceOption {
	return func(s *service) {
		s.metricsProvider = metricsProvider
	}
}

// WithServiceUploadManager supplies the storage artifacts are read from.
//
// It must be the same storage, and the same path prefix, that the Worker writes
// to. Nothing here can check that, and a mismatch surfaces as an artifact that
// exists in the bucket and cannot be found by the service that promised it.
func WithServiceUploadManager(manager uploads.UploadManager) ServiceOption {
	return func(s *service) {
		if manager != nil {
			s.uploader = manager
		}
	}
}

// WithServiceCompressor supplies the compressor artifacts were written with. It
// must match the Worker's, or Open returns garbage.
func WithServiceCompressor(compressor compression.Compressor) ServiceOption {
	return func(s *service) {
		if compressor != nil {
			s.packager.compressor = compressor
		}
	}
}

// WithServiceDecryptor supplies the decryptor for artifacts written encrypted.
// It must match the Worker's encryptor.
//
// Setting it also disables Download: see ErrArtifactEncrypted.
func WithServiceDecryptor(decryptor encryption.Decryptor) ServiceOption {
	return func(s *service) {
		if decryptor != nil {
			s.packager.decryptor = decryptor
			// Recorded so Download can refuse rather than hand out a link to
			// ciphertext. The Service never encrypts — only the Worker does —
			// but a configured decryptor is proof that the Worker encrypts, and
			// is the only evidence of that available on this side.
			s.packager.encryptor = encryptorPresent{}
		}
	}
}

// WithServiceAuditRecorder attaches the audit log this package writes to.
//
// Every submission and every state change it drives is recorded. That is not
// decoration: an export artifact is the most sensitive object an application
// produces, and a system that can produce one without leaving a record of who
// asked has a data exfiltration path with no alarm on it.
func WithServiceAuditRecorder(recorder audit.Recorder) ServiceOption {
	return func(s *service) {
		if recorder != nil {
			s.recorder = recorder
		}
	}
}

// WithActorResolver supplies the principal recorded in audit entries.
func WithActorResolver(resolver ActorResolver) ServiceOption {
	return func(s *service) {
		if resolver != nil {
			s.actor = resolver
		}
	}
}

// SQLStoreOption configures a SQL Store.
type SQLStoreOption func(*sqlStore)

// WithTablePrefix overrides DefaultTablePrefix. It must be a plain SQL
// identifier fragment: it is interpolated into the query text, not bound as a
// parameter, and it must match the prefix the migrations were rendered with.
func WithTablePrefix(prefix string) SQLStoreOption {
	return func(s *sqlStore) {
		if prefix != "" {
			s.tables = newTables(prefix)
		}
	}
}

// WithStoreLogger attaches a logger.
func WithStoreLogger(logger logging.Logger) SQLStoreOption {
	return func(s *sqlStore) {
		s.logger = logger
	}
}

// WithStoreTracerProvider attaches a tracer provider.
func WithStoreTracerProvider(tracerProvider tracing.TracerProvider) SQLStoreOption {
	return func(s *sqlStore) {
		s.tracerProvider = tracerProvider
	}
}

// WithStoreMetricsProvider attaches a metrics provider.
func WithStoreMetricsProvider(metricsProvider metrics.Provider) SQLStoreOption {
	return func(s *sqlStore) {
		s.metricsProvider = metricsProvider
	}
}

// SweeperOption configures a Sweeper.
type SweeperOption func(*Sweeper)

// WithSweeperClock swaps the clock deciding what has expired.
func WithSweeperClock(c clock.Clock) SweeperOption {
	return func(s *Sweeper) {
		if c != nil {
			s.clock = c
		}
	}
}

// WithSweeperLogger attaches a logger.
func WithSweeperLogger(logger logging.Logger) SweeperOption {
	return func(s *Sweeper) {
		s.logger = logger
	}
}

// WithSweeperTracerProvider attaches a tracer provider.
func WithSweeperTracerProvider(tracerProvider tracing.TracerProvider) SweeperOption {
	return func(s *Sweeper) {
		s.tracerProvider = tracerProvider
	}
}

// WithSweeperMetricsProvider attaches a metrics provider, enabling the overdue
// gauge — which is the one instrument in this package worth alerting on.
func WithSweeperMetricsProvider(metricsProvider metrics.Provider) SweeperOption {
	return func(s *Sweeper) {
		s.metricsProvider = metricsProvider
	}
}

// WithSweeperUploadManager supplies the storage artifacts are deleted from. It
// must be the same storage the Worker writes to.
//
// Without it the Sweeper refuses to expire artifacts at all rather than marking
// rows expired against objects it cannot delete — a row that says the artifact
// is gone while the artifact is not is worse than no sweep, because it stops
// anybody looking.
func WithSweeperUploadManager(manager uploads.UploadManager) SweeperOption {
	return func(s *Sweeper) {
		if manager != nil {
			s.uploader = manager
		}
	}
}

// WorkerOption configures a Worker.
type WorkerOption func(*Worker)

// WithWorkerClock swaps the clock driving the poll loop, leases, and backoff.
func WithWorkerClock(c clock.Clock) WorkerOption {
	return func(w *Worker) {
		if c != nil {
			w.clock = c
		}
	}
}

// WithWorkerLogger attaches a logger. A failing collector is reported through
// it and nowhere else — there is no caller to return it to — so without one a
// domain that has been failing to collect for a week is visible only in
// metrics.
func WithWorkerLogger(logger logging.Logger) WorkerOption {
	return func(w *Worker) {
		w.logger = logger
	}
}

// WithWorkerTracerProvider attaches a tracer provider. Cycles that claim
// nothing are not traced — a root span every poll interval is noise.
func WithWorkerTracerProvider(tracerProvider tracing.TracerProvider) WorkerOption {
	return func(w *Worker) {
		w.tracerProvider = tracerProvider
	}
}

// WithWorkerMetricsProvider attaches a metrics provider.
func WithWorkerMetricsProvider(metricsProvider metrics.Provider) WorkerOption {
	return func(w *Worker) {
		w.metricsProvider = metricsProvider
	}
}

// WithWorkerUploadManager supplies the storage artifacts are written to.
// Required for exports; an erasure-only Worker does not need it.
func WithWorkerUploadManager(manager uploads.UploadManager) WorkerOption {
	return func(w *Worker) {
		if manager != nil {
			w.uploader = manager
		}
	}
}

// WithWorkerCompressor compresses artifacts before they are stored.
//
// Worth setting. An export is JSON assembled from every domain in an
// application, which is the most compressible shape there is — and the artifact
// is written once and read at most once, so the compression is nearly free.
func WithWorkerCompressor(compressor compression.Compressor) WorkerOption {
	return func(w *Worker) {
		if compressor != nil {
			w.packager.compressor = compressor
		}
	}
}

// WithWorkerEncryptor encrypts artifacts at rest.
//
// It changes what delivery is possible: an encrypted artifact cannot be handed
// out as a signed URL, because the subject would receive ciphertext. See
// ErrArtifactEncrypted. Configure the Service with the matching decryptor.
func WithWorkerEncryptor(encryptor encryption.Encryptor) WorkerOption {
	return func(w *Worker) {
		if encryptor != nil {
			w.packager.encryptor = encryptor
		}
	}
}

// WithWorkerNotifier supplies who to tell when a request finishes.
func WithWorkerNotifier(notifier Notifier) WorkerOption {
	return func(w *Worker) {
		if notifier != nil {
			w.notifier = notifier
		}
	}
}

// WithWorkerAuditRecorder attaches the audit log completions are recorded in.
//
// The completion entry is the one that says what was actually disclosed or
// destroyed, and it is written in the same transaction as the state change it
// describes.
func WithWorkerAuditRecorder(recorder audit.Recorder) WorkerOption {
	return func(w *Worker) {
		if recorder != nil {
			w.recorder = recorder
		}
	}
}

// WithWorkerActorResolver supplies the principal recorded in audit entries.
func WithWorkerActorResolver(resolver ActorResolver) WorkerOption {
	return func(w *Worker) {
		if resolver != nil {
			w.actor = resolver
		}
	}
}

// WithWorkerURLSigner supplies how a notification's download URL is minted.
//
// It exists so the Worker can hand the subject a link without holding a
// Service — which would be circular, since a Service is the thing that reads
// what this Worker writes. The signer returns the URL and its expiry; an empty
// URL means the notification carries no link, which is correct for encrypted
// artifacts and for providers that cannot sign.
func WithWorkerURLSigner(signer func(ctx context.Context, req *Request) (url string, expiresAt time.Time)) WorkerOption {
	return func(w *Worker) {
		if signer != nil {
			w.signer = signer
		}
	}
}
