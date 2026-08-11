package asynccfg

import (
	"context"
	"strings"

	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/notifications/async"
	"github.com/primandproper/platform-go/v10/notifications/async/ably"
	"github.com/primandproper/platform-go/v10/notifications/async/noop"
	"github.com/primandproper/platform-go/v10/notifications/async/pusher"
	asyncsse "github.com/primandproper/platform-go/v10/notifications/async/sse"
	asyncws "github.com/primandproper/platform-go/v10/notifications/async/websocket"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// The providers split into two classes that this constant block is the only
// place anyone chooses between, and the difference is a deployment constraint
// rather than a preference:
//
//   - pusher and ably are fleet-safe. A hosted broker holds the connections, so
//     any replica can publish to a client connected to any other.
//   - websocket and sse hold connections in the publishing process, and are
//     therefore correct only at one replica. At two, a client connected to one
//     instance silently misses every event published on another. See those
//     packages' docs for why there is no messagequeue backplane that would lift
//     the constraint.
const (
	// ProviderPusher is the Pusher provider. Fleet-safe.
	ProviderPusher = "pusher"
	// ProviderAbly is the Ably provider. Fleet-safe.
	ProviderAbly = "ably"
	// ProviderWebSocket is the WebSocket provider. Correct at one replica only.
	ProviderWebSocket = "websocket"
	// ProviderSSE is the SSE provider. Correct at one replica only.
	ProviderSSE = "sse"
	// ProviderNoop is the no-op provider.
	ProviderNoop = "noop"
)

const (
	// TopologySingleReplica declares that exactly one replica of this service
	// runs. It is the only topology the self-hosted providers support.
	TopologySingleReplica = "single_replica"
	// TopologyFleet declares that more than one replica runs, which requires a
	// provider whose broker is outside this process.
	TopologyFleet = "fleet"
)

// selfHosted reports whether a provider holds its client connections in this
// process's memory, which is what makes replica count load-bearing.
func selfHosted(provider string) bool {
	switch cleanProvider(provider) {
	case ProviderSSE, ProviderWebSocket:
		return true
	default:
		return false
	}
}

func cleanProvider(provider string) string {
	return strings.TrimSpace(strings.ToLower(provider))
}

var (
	// ErrTopologyRequired is returned when a self-hosted provider is selected
	// without declaring a Topology.
	//
	// A process cannot count its own replicas, so nothing here can tell a
	// correct single-replica deployment from one that silently lost half its
	// notifications when it scaled to two. Requiring the declaration is what
	// converts that into a decision somebody made on purpose.
	ErrTopologyRequired = errors.New("sse and websocket require an explicit topology declaration")

	// ErrFleetUnsupportedForSelfHostedProvider is returned when a self-hosted
	// provider is paired with TopologyFleet.
	//
	// sse and websocket hold connections in process memory, so a Publish on one
	// replica cannot reach subscribers on another. Use pusher or ably for a
	// fleet: their brokers hold the connections and already have the semantics
	// this combination is reaching for.
	ErrFleetUnsupportedForSelfHostedProvider = errors.New("sse and websocket cannot serve a fleet; use a hosted provider")
)

type (
	// Config is the configuration for the async notifications provider.
	Config struct {
		Pusher    *pusher.Config   `env:",init"    envPrefix:"PUSHER_"       json:"pusher,omitempty"    yaml:"pusher,omitempty"`
		Ably      *ably.Config     `env:",init"    envPrefix:"ABLY_"         json:"ably,omitempty"      yaml:"ably,omitempty"`
		WebSocket *asyncws.Config  `env:",init"    envPrefix:"WEBSOCKET_"    json:"websocket,omitempty" yaml:"websocket,omitempty"`
		SSE       *asyncsse.Config `env:",init"    envPrefix:"SSE_"          json:"sse,omitempty"       yaml:"sse,omitempty"`
		Provider  string           `env:"PROVIDER" json:"provider,omitempty" yaml:"provider,omitempty"`

		// Topology declares how many replicas of this service run. It is
		// required for the self-hosted providers and ignored for the rest,
		// which are correct at any replica count.
		//
		// See the async package documentation for why this is declared rather
		// than detected.
		Topology string `env:"TOPOLOGY" json:"topology,omitempty" yaml:"topology,omitempty"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// validateTopology reports whether Provider and Topology agree.
//
// It is separate from ValidateWithContext because NewAsyncNotifier applies it
// too: a Config reaching the constructor without having been validated is the
// case where a silent single-replica assumption would otherwise survive.
func (cfg *Config) validateTopology() error {
	if !selfHosted(cfg.Provider) {
		return nil
	}

	switch cfg.Topology {
	case TopologySingleReplica:
		return nil
	case TopologyFleet:
		return errors.Wrapf(ErrFleetUnsupportedForSelfHostedProvider, "provider %q", cfg.Provider)
	default:
		// The observed value goes in the message because this branch also
		// catches a typo, and "requires a declaration" alone reads as a denial
		// that one was made.
		return errors.Wrapf(ErrTopologyRequired, "provider %q, topology %q", cfg.Provider, cfg.Topology)
	}
}

// ValidateWithContext validates a Config struct.
//
// The sub-configs for providers that were not selected are skipped rather than
// merely unguarded: ozzo validates any non-nil pointer to a Validatable once a
// field's rules have run, and `env:",init"` leaves every sub-config non-nil. A
// validation.When guard alone stops the Required rule and nothing else, so
// Pusher's and Ably's credentials were required at once and no config could load.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	if err := validation.ValidateStructWithContext(ctx, cfg,
		// Required as well as constrained: ozzo's In skips empty values, so an
		// unset provider used to validate cleanly and then select noop, which is
		// indistinguishable from a deployment that meant to turn notifications
		// off. Not sending them has to be asked for by name.
		validation.Field(&cfg.Provider, validation.Required, validation.In(ProviderPusher, ProviderAbly, ProviderWebSocket, ProviderSSE, ProviderNoop)),
		validation.Field(&cfg.Topology, validation.In(TopologySingleReplica, TopologyFleet, "")),
		validation.Field(&cfg.Pusher, validation.Skip.When(cfg.Provider != ProviderPusher), validation.Required),
		validation.Field(&cfg.Ably, validation.Skip.When(cfg.Provider != ProviderAbly), validation.Required),
		validation.Field(&cfg.WebSocket, validation.Skip.When(cfg.Provider != ProviderWebSocket), validation.Required),
	); err != nil {
		return err
	}

	return cfg.validateTopology()
}

// NewAsyncNotifier provides an AsyncNotifier based on configuration.
//
// A self-hosted provider without an agreeing Topology is refused here as well
// as in ValidateWithContext, since this is reachable without it.
//
// Each branch builds into a variable and returns only once the error is known
// to be nil: the provider constructors hand back their own concrete pointer
// type, and returning one straight through would turn a nil *Notifier into a
// non-nil async.AsyncNotifier on the error path.
func (cfg *Config) NewAsyncNotifier(opts ...Option) (async.AsyncNotifier, error) {
	if err := cfg.validateTopology(); err != nil {
		return nil, err
	}

	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	switch cleanProvider(cfg.Provider) {
	case ProviderPusher:
		n, err := pusher.NewNotifier(cfg.Pusher, pusher.WithLogger(logger), pusher.WithTracerProvider(tracerProvider), pusher.WithMetricsProvider(metricsProvider))
		if err != nil {
			return nil, err
		}

		return n, nil
	case ProviderAbly:
		n, err := ably.NewNotifier(cfg.Ably, ably.WithLogger(logger), ably.WithTracerProvider(tracerProvider), ably.WithMetricsProvider(metricsProvider))
		if err != nil {
			return nil, err
		}

		return n, nil
	case ProviderWebSocket:
		n, err := asyncws.NewNotifier(cfg.WebSocket, asyncws.WithLogger(logger), asyncws.WithTracerProvider(tracerProvider))
		if err != nil {
			return nil, err
		}

		return n, nil
	case ProviderSSE:
		n, err := asyncsse.NewNotifier(cfg.SSE, asyncsse.WithLogger(logger), asyncsse.WithTracerProvider(tracerProvider))
		if err != nil {
			return nil, err
		}

		return n, nil
	case ProviderNoop:
		// Only by name. An unset provider falls through to the error below,
		// because "notify nobody, forever" is a decision somebody has to make.
		return noop.NewAsyncNotifier()
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "async notifications provider %q", cfg.Provider)
	}
}
