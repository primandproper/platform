package eventstreamcfg

import (
	"context"
	"strings"

	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/eventstream"
	"github.com/primandproper/platform-go/v10/eventstream/sse"
	"github.com/primandproper/platform-go/v10/eventstream/websocket"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderSSE is the SSE provider.
	ProviderSSE = "sse"
	// ProviderWebSocket is the websocket provider.
	ProviderWebSocket = "websocket"
)

type (
	// Config is the configuration for the event stream provider.
	Config struct {
		WebSocket *websocket.Config `env:",init"    envPrefix:"WEBSOCKET_"    json:"websocket,omitempty" yaml:"websocket,omitempty"`
		Provider  string            `env:"PROVIDER" json:"provider,omitempty" yaml:"provider,omitempty"`
	}
)

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config struct.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.In(ProviderSSE, ProviderWebSocket)),
		validation.Field(&cfg.WebSocket, validation.When(cfg.Provider == ProviderWebSocket, validation.Required)),
	)
}

// NewEventStreamUpgrader provides an EventStreamUpgrader based on configuration.
func NewEventStreamUpgrader(_ context.Context, cfg *Config, opts ...Option) (eventstream.EventStreamUpgrader, error) {
	o := newOptions(opts)
	logger, tracerProvider := o.logger, o.tracerProvider

	switch strings.TrimSpace(strings.ToLower(cfg.Provider)) {
	case ProviderSSE:
		return sse.NewUpgrader(sse.WithTracerProvider(tracerProvider)), nil
	case ProviderWebSocket:
		return websocket.NewUpgrader(cfg.WebSocket, websocket.WithLogger(logger), websocket.WithTracerProvider(tracerProvider)), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "eventstream provider %q", cfg.Provider)
	}
}

// NewBidirectionalEventStreamUpgrader provides a BidirectionalEventStreamUpgrader based on configuration.
func NewBidirectionalEventStreamUpgrader(_ context.Context, cfg *Config, opts ...Option) (eventstream.BidirectionalEventStreamUpgrader, error) {
	o := newOptions(opts)
	logger, tracerProvider := o.logger, o.tracerProvider

	switch strings.TrimSpace(strings.ToLower(cfg.Provider)) {
	case ProviderSSE:
		return nil, errors.New("SSE does not support bidirectional event streams")
	case ProviderWebSocket:
		return websocket.NewUpgrader(cfg.WebSocket, websocket.WithLogger(logger), websocket.WithTracerProvider(tracerProvider)), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "eventstream provider %q", cfg.Provider)
	}
}
