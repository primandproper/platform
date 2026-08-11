package pprof

import (
	"context"
	"net/http"
	"net/http/pprof"
	"runtime"
	"strconv"
	"time"

	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/profiling"
)

// NewProfilingProvider creates a pprof-based profiling provider that exposes
// /debug/pprof endpoints on an HTTP server.
//
// A nil Config is an error rather than a noop, for the same reason the pyroscope
// provider refuses one: a provider that profiles nothing is indistinguishable
// from one that works until somebody goes looking for a profile.
func NewProfilingProvider(ctx context.Context, logger logging.Logger, cfg *Config) (*Provider, error) {
	if cfg == nil {
		return nil, errors.Wrap(errors.ErrNilInputParameter, "nil pprof config")
	}

	port := cfg.Port
	if port == 0 {
		port = DefaultPort
	}

	if cfg.EnableMutexProfile {
		runtime.SetMutexProfileFraction(5)
	}
	if cfg.EnableBlockProfile {
		runtime.SetBlockProfileRate(5)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	addr := ":" + strconv.FormatUint(uint64(port), 10)
	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Addr:              addr,
		Handler:           mux,
	}

	logger.WithValue("port", port).
		WithValue("addr", addr).
		Info("starting pprof HTTP server")

	return &Provider{
		server: server,
		logger: logger,
	}, nil
}

var _ profiling.Provider = (*Provider)(nil)

// Provider is the net/http/pprof profiling.Provider implementation. It is
// exported, and returned by NewProfilingProvider, so a caller who has chosen
// pprof can depend on that choice rather than on the interface every profiler
// shares.
type Provider struct {
	server *http.Server
	logger logging.Logger
}

func (p *Provider) Start(ctx context.Context) error {
	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			p.logger.Error("pprof server error", err)
		}
	}()
	return nil
}

func (p *Provider) Shutdown(ctx context.Context) error {
	if p.server != nil {
		if err := p.server.Shutdown(ctx); err != nil {
			return errors.Wrap(err, "shutting down pprof server")
		}
		p.logger.Info("stopped pprof server")
	}
	return nil
}
