package routingcfg

import (
	"context"

	"github.com/primandproper/platform-go/v9/encoding"
	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/routing"
	"github.com/primandproper/platform-go/v9/routing/backends/chi"
	"github.com/primandproper/platform-go/v9/routing/backends/gin"
	"github.com/primandproper/platform-go/v9/routing/backends/httprouter"
	"github.com/primandproper/platform-go/v9/routing/backends/stdlib"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderChi is the string we use to refer to chi.
	ProviderChi = "chi"
	// ProviderStdlib is the string we use to refer to the net/http.ServeMux backend.
	ProviderStdlib = "stdlib"
	// ProviderHTTPRouter is the string we use to refer to the julienschmidt/httprouter backend.
	ProviderHTTPRouter = "httprouter"
	// ProviderGin is the string we use to refer to the gin-gonic/gin backend.
	ProviderGin = "gin"
)

// Config configures our router.
type Config struct {
	_ struct{} `json:"-" yaml:"-"`

	Chi        *chi.Config        `env:",init"    envPrefix:"CHI_"          json:"chiConfig,omitempty"        yaml:"chiConfig,omitempty"`
	Stdlib     *stdlib.Config     `env:",init"    envPrefix:"STDLIB_"       json:"stdlibConfig,omitempty"     yaml:"stdlibConfig,omitempty"`
	HTTPRouter *httprouter.Config `env:",init"    envPrefix:"HTTPROUTER_"   json:"httpRouterConfig,omitempty" yaml:"httpRouterConfig,omitempty"`
	Gin        *gin.Config        `env:",init"    envPrefix:"GIN_"          json:"ginConfig,omitempty"        yaml:"ginConfig,omitempty"`
	Provider   string             `env:"PROVIDER" json:"provider,omitempty" yaml:"provider,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a router config struct.
//
// Provider is Required as well as constrained: ozzo's In skips empty values, so
// without it an unset provider validated cleanly and then matched no dispatch
// case.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.Required, validation.In(ProviderChi, ProviderStdlib, ProviderHTTPRouter, ProviderGin)),
	)
}

// NewBackend provides a routing.Backend from a routing config, selecting the
// underlying router library by provider.
func NewBackend(_ context.Context, cfg *Config, opts ...Option) (routing.Backend, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricProvider := o.logger, o.tracerProvider, o.metricsProvider

	switch cfg.Provider {
	case ProviderChi:
		return chi.NewBackend(cfg.Chi, chi.WithLogger(logger), chi.WithTracerProvider(tracerProvider), chi.WithMetricsProvider(metricProvider)), nil
	case ProviderStdlib:
		return stdlib.NewBackend(cfg.Stdlib, stdlib.WithLogger(logger), stdlib.WithTracerProvider(tracerProvider), stdlib.WithMetricsProvider(metricProvider)), nil
	case ProviderHTTPRouter:
		return httprouter.NewBackend(cfg.HTTPRouter, httprouter.WithLogger(logger), httprouter.WithTracerProvider(tracerProvider), httprouter.WithMetricsProvider(metricProvider)), nil
	case ProviderGin:
		return gin.NewBackend(cfg.Gin, gin.WithLogger(logger), gin.WithTracerProvider(tracerProvider), gin.WithMetricsProvider(metricProvider)), nil
	default:
		return nil, errors.Newf("unknown provider: %s", cfg.Provider)
	}
}

// NewRouter provides a fully-wired *routing.Router from a routing config: it
// selects the backend by provider and layers the declarative Router on top.
func NewRouter(
	ctx context.Context,
	cfg *Config,
	enc encoding.ServerEncoderDecoder,
	opts ...Option,
) (*routing.Router, error) {
	o := newOptions(opts)

	backend, err := NewBackend(ctx, cfg, opts...)
	if err != nil {
		return nil, err
	}

	routerOpts := append([]routing.RouterOption{routing.WithLogger(o.logger), routing.WithTracerProvider(o.tracerProvider)}, o.router...)

	return routing.New(backend, enc, routerOpts...), nil
}
