package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"time"

	perrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

const (
	defaultServiceName = "grpc_server"
)

type (
	Config struct {
		TLSCertificateFile    string `env:"TLS_CERTIFICATE_FILEPATH"     json:"tlsCertificate,omitempty"    yaml:"tlsCertificate,omitempty"`
		TLSCertificateKeyFile string `env:"TLS_CERTIFICATE_KEY_FILEPATH" json:"tlsCertificateKey,omitempty" yaml:"tlsCertificateKey,omitempty"`
		Port                  uint16 `env:"PORT"                         json:"port"                        yaml:"port"`
	}

	Server struct {
		logger         logging.Logger
		config         *Config
		grpcServer     *grpc.Server
		tracerProvider tracing.TracerProvider
	}

	// RegistrationFunc is i.e. protobuf.RegisterSomeExampleServiceServer(grpcServer, &exampleServiceServerImpl{}).
	RegistrationFunc func(*grpc.Server)
)

func NewGRPCServer(
	cfg *Config,
	unaryServerInterceptors []grpc.UnaryServerInterceptor,
	streamServerInterceptors []grpc.StreamServerInterceptor,
	registrationFunctions []RegistrationFunc,
	opts ...Option,
) (*Server, error) {
	if cfg == nil {
		return nil, perrors.ErrNilInputParameter
	}

	o := newOptions(opts)
	logger := o.logger

	tp := tracing.EnsureTracerProvider(o.tracerProvider)
	serverOpts := []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler(otelgrpc.WithTracerProvider(tp))),
		grpc.ChainUnaryInterceptor(append([]grpc.UnaryServerInterceptor{LoggingInterceptor(logger)}, unaryServerInterceptors...)...),
		grpc.ChainStreamInterceptor(streamServerInterceptors...),
	}

	if cfg.TLSCertificateKeyFile != "" && cfg.TLSCertificateFile != "" {
		serverCert, err := tls.LoadX509KeyPair(cfg.TLSCertificateFile, cfg.TLSCertificateKeyFile)
		if err != nil {
			return nil, err
		}

		config := &tls.Config{
			Certificates: []tls.Certificate{serverCert},
			ClientAuth:   tls.NoClientCert,
			CurvePreferences: []tls.CurveID{
				tls.CurveP256,
				tls.X25519,
			},
			MinVersion: tls.VersionTLS12,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			},
		}

		serverOpts = append(serverOpts, grpc.Creds(credentials.NewTLS(config)))
	}

	grpcServer := grpc.NewServer(serverOpts...)
	for _, rf := range registrationFunctions {
		rf(grpcServer)
	}

	if o.reflection {
		reflection.Register(grpcServer)
	}

	name := defaultServiceName
	if o.serviceName != "" {
		name = o.serviceName
	}

	return &Server{
		logger:         logging.NewNamedLogger(logger, name),
		config:         cfg,
		grpcServer:     grpcServer,
		tracerProvider: tp,
	}, nil
}

// Shutdown stops the server gracefully, then flushes and shuts down the tracer
// provider — the same order as the HTTP sibling, and for the same reason: spans
// from RPCs that complete during draining are lost if the flush runs first.
//
// In-flight RPCs are given until ctx is done to finish. If ctx expires first the
// server is stopped hard and the context's error is returned, so a caller can
// tell a clean drain from a forced one.
func (s *Server) Shutdown(ctx context.Context) error {
	stopped := make(chan struct{})
	go func() {
		s.grpcServer.GracefulStop()
		close(stopped)
	}()

	var err error
	select {
	case <-stopped:
	case <-ctx.Done():
		// GracefulStop is still blocked on in-flight RPCs; Stop unblocks it by
		// cancelling them.
		s.grpcServer.Stop()
		<-stopped
		err = ctx.Err()
	}

	if flushErr := s.tracerProvider.ForceFlush(ctx); flushErr != nil {
		s.logger.Error("flushing traces", flushErr)
	}

	if shutdownErr := s.tracerProvider.Shutdown(ctx); shutdownErr != nil {
		s.logger.Error("shutting down tracer provider", shutdownErr)
	}

	return err
}

// Serve serves gRPC traffic until Shutdown is called or ctx is done.
//
// A graceful stop reports nil; every other failure is returned. It used to
// return nothing, and the only sentinel it checked was net/http's
// ErrServerClosed — which gRPC never returns — so a bind failure or a dead
// server was completely silent.
func (s *Server) Serve(ctx context.Context) error {
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", s.config.Port))
	if err != nil {
		return perrors.Wrap(err, "binding gRPC listener")
	}

	s.logger.WithValue("port", s.config.Port).Info("Listening for GRPC requests")

	// grpc.ErrServerStopped is what Stop and GracefulStop produce, and is the
	// only "this is a normal shutdown" answer this server can get.
	if err = s.grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return perrors.Wrap(err, "serving gRPC traffic")
	}

	return nil
}

func LoggingInterceptor(logger logging.Logger) grpc.UnaryServerInterceptor {
	l := logging.EnsureLogger(logger)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		var ev uint8

		start := time.Now()
		result, err := handler(ctx, req)
		end := time.Since(start)

		if err != nil {
			ev = 1
		}

		l.WithValues(map[string]any{
			"rpc.method": info.FullMethod,
			"elapsed":    end,
			"error":      ev,
		}).Info("rpc invoked")

		return result, err
	}
}
