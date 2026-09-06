package grpc

import (
	"context"
	"time"

	"github.com/primandproper/platform-go/v14/observability"
	"github.com/primandproper/platform-go/v14/observability/logging"
	"github.com/primandproper/platform-go/v14/observability/metrics"
	"github.com/primandproper/platform-go/v14/observability/tracing"
	"github.com/primandproper/platform-go/v14/random"
)

// DefaultInvitationTTL is how long an invitation lives when a client sends no
// expiry of its own.
//
// A week, and it is a default rather than a policy this package holds: a
// consumer with an opinion says so with WithInvitationTTL, and a client with
// one puts an expires_at on the request. What a transport cannot do is decline
// to pick — an invitation with no expiry is a link that works forever, which is
// the one answer that is wrong for everybody.
const DefaultInvitationTTL = 7 * 24 * time.Hour

// defaultInvitationTokenBytes is the entropy behind an invitation link. Thirty-two
// bytes is what the rest of this module mints single-use tokens with.
const defaultInvitationTokenBytes = 32

// TokenMinter produces the value an invitation link carries.
//
// It is a seam rather than a fixed implementation because the token is what
// reaches a recipient, and a consumer whose links are signed, prefixed, or
// minted by a service of their own needs to say so. The default is this
// module's CSPRNG, which is not a policy choice — a guessable invitation token
// is an account takeover, and there is no second reasonable answer — where the
// lifetime above genuinely is one.
type TokenMinter func(ctx context.Context) (string, error)

// Option configures a Server.
type Option func(*Server)

// WithLogger sets the logger. Absent means no logging.
func WithLogger(logger logging.Logger) Option {
	return func(s *Server) { s.logger = logger }
}

// WithTracerProvider sets the tracer provider. Absent means no tracing.
//
// It is a provider rather than a ready-made tracer so that the instrumentation
// scope of this package's spans is decided here rather than by the caller.
func WithTracerProvider(tracerProvider tracing.Provider) Option {
	return func(s *Server) { s.tracerProvider = tracerProvider }
}

// WithMetricsProvider sets the metrics provider. Absent means no metrics.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(s *Server) { s.metricsProvider = metricsProvider }
}

// WithPillars supplies logger, tracer provider and metrics provider at once.
//
// Options apply in order, so WithPillars(p) followed by WithMetricsProvider(nil)
// leaves this one component unmetered.
func WithPillars(p *observability.Pillars) Option {
	return func(s *Server) {
		s.logger, s.tracerProvider, s.metricsProvider = p.Deps()
	}
}

// WithInvitationTTL sets how long an invitation lives when the request names no
// expiry. A non-positive duration is ignored, leaving DefaultInvitationTTL.
func WithInvitationTTL(ttl time.Duration) Option {
	return func(s *Server) {
		if ttl > 0 {
			s.invitationTTL = ttl
		}
	}
}

// WithTokenMinter sets what mints an invitation's token. A nil minter is
// ignored, leaving the default CSPRNG.
func WithTokenMinter(mint TokenMinter) Option {
	return func(s *Server) {
		if mint != nil {
			s.mintToken = mint
		}
	}
}

// defaultTokenMinter is the CSPRNG the module already uses for single-use
// tokens.
func defaultTokenMinter(ctx context.Context) (string, error) {
	return random.GenerateBase64EncodedString(ctx, defaultInvitationTokenBytes)
}
