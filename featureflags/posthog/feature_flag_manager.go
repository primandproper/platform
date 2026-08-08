package posthog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/primandproper/platform-go/v10/circuitbreaking"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/featureflags"
	"github.com/primandproper/platform-go/v10/identifiers"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/keys"
	"github.com/primandproper/platform-go/v10/observability/metrics"

	openfeatureposthog "github.com/dhaus67/openfeature-posthog-go"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/posthog/posthog-go"
)

const (
	serviceName  = "posthog_feature_flag_manager"
	clientDomain = "posthog_feature_flags"
)

var (
	ErrNilConfig          = platformerrors.New("missing config")
	ErrMissingCredentials = platformerrors.New("missing PostHog credentials")
)

type (
	// featureFlagManager implements the feature flag interface using OpenFeature.
	featureFlagManager struct {
		o11y           observability.Observer
		posthogClient  posthog.Client
		circuitBreaker circuitbreaking.CircuitBreaker
		evalCounter    metrics.Int64Counter
		errorCounter   metrics.Int64Counter
		latencyHist    metrics.Float64Histogram
		ofClient       *openfeature.Client
		domain         string
	}
)

// NewFeatureFlagManager constructs a new featureFlagManager backed by OpenFeature.
func NewFeatureFlagManager(cfg *Config, circuitBreaker circuitbreaking.CircuitBreaker, opts ...Option) (featureflags.FeatureFlagManager, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}

	if cfg.ProjectAPIKey == "" {
		return nil, platformerrors.Wrap(ErrMissingCredentials, "missing credential 'ProjectAPIKey'")
	}

	if cfg.PersonalAPIKey == "" {
		return nil, platformerrors.Wrap(ErrMissingCredentials, "missing credential 'PersonalAPIKey'")
	}

	o := newOptions(opts)

	// Built before anything that can fail: the teardown paths below log, and an
	// absent logger must log nowhere rather than panic on the one path that
	// exists to clean up after another failure.
	o11y := observability.NewObserver(serviceName, o.logger, o.tracerProvider)

	// Create the metric instruments before the client/provider so a counter failure
	// returns without having registered a global provider or opened a client to leak.
	mp := metrics.EnsureMetricsProvider(o.metricsProvider)

	evalCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_evaluations", serviceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating eval counter")
	}

	errorCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_errors", serviceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating error counter")
	}

	latencyHist, err := mp.NewFloat64Histogram(fmt.Sprintf("%s_latency_ms", serviceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating latency histogram")
	}

	// Each manager binds its own OpenFeature domain so a second manager can't rebind
	// the domain (and thus the provider/client) out from under an existing one.
	domain := fmt.Sprintf("%s_%s", clientDomain, identifiers.New())

	phc := posthog.Config{
		PersonalApiKey: cfg.PersonalAPIKey,
		Endpoint:       cfg.Endpoint,
	}

	for _, modifier := range o.configModifiers {
		modifier(&phc)
	}

	client, err := posthog.NewWithConfig(
		cfg.ProjectAPIKey,
		phc,
	)
	if err != nil {
		return nil, platformerrors.Wrap(err, "failed to create posthog client")
	}

	provider := openfeatureposthog.NewProvider(client)
	if err = openfeature.SetNamedProviderAndWait(domain, provider); err != nil {
		if closeErr := client.Close(); closeErr != nil {
			o11y.Logger().Error("error closing OpenFeatureFlag client", closeErr)
		}
		return nil, platformerrors.Wrap(err, "failed to set OpenFeature provider")
	}

	ofClient := openfeature.NewClient(domain)

	ffm := &featureFlagManager{
		domain:         domain,
		posthogClient:  client,
		ofClient:       ofClient,
		circuitBreaker: circuitBreaker,
		o11y:           o11y,
		evalCounter:    evalCounter,
		errorCounter:   errorCounter,
		latencyHist:    latencyHist,
	}

	return ffm, nil
}

// toOpenFeatureContext converts a featureflags.EvaluationContext into the SDK's
// own representation. It is the only place this provider crosses the boundary
// between the platform-owned type and the OpenFeature type.
func toOpenFeatureContext(evalCtx featureflags.EvaluationContext) openfeature.EvaluationContext {
	return openfeature.NewEvaluationContext(evalCtx.TargetingKey, evalCtx.Attributes)
}

// CanUseFeature returns whether the supplied evaluation context is permitted to use
// the named feature.
func (f *featureFlagManager) CanUseFeature(ctx context.Context, feature string, evalCtx featureflags.EvaluationContext) (bool, error) {
	ctx, op := f.o11y.Begin(ctx,
		observability.WithValue(keys.UserIDKey, evalCtx.TargetingKey),
		observability.WithValue("feature", feature),
	)
	defer op.End()

	if !f.circuitBreaker.CanProceed() {
		return false, circuitbreaking.ErrCircuitBroken
	}

	startTime := time.Now()
	flagEnabled, err := f.ofClient.BooleanValue(ctx, feature, false, toOpenFeatureContext(evalCtx))
	f.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	if err != nil {
		f.errorCounter.Add(ctx, 1)
		f.circuitBreaker.Failed()
		return false, op.Error(err, "checking feature flag eligibility")
	}

	op.Set("flag.value", flagEnabled)

	f.evalCounter.Add(ctx, 1)
	f.circuitBreaker.Succeeded()
	return flagEnabled, nil
}

// GetStringValue returns the string value of a feature flag, falling back to
// defaultValue on error.
func (f *featureFlagManager) GetStringValue(ctx context.Context, feature, defaultValue string, evalCtx featureflags.EvaluationContext) (string, error) {
	ctx, op := f.o11y.Begin(ctx,
		observability.WithValue(keys.UserIDKey, evalCtx.TargetingKey),
		observability.WithValue("feature", feature),
	)
	defer op.End()

	if !f.circuitBreaker.CanProceed() {
		return defaultValue, circuitbreaking.ErrCircuitBroken
	}

	startTime := time.Now()
	result, err := f.ofClient.StringValue(ctx, feature, defaultValue, toOpenFeatureContext(evalCtx))
	f.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	if err != nil {
		f.errorCounter.Add(ctx, 1)
		f.circuitBreaker.Failed()
		return defaultValue, op.Error(err, "checking feature flag string variation")
	}

	op.Set("flag.default", defaultValue).Set("flag.value", result)

	f.evalCounter.Add(ctx, 1)
	f.circuitBreaker.Succeeded()
	return result, nil
}

// GetInt64Value returns the int64 value of a feature flag, falling back to
// defaultValue on error.
func (f *featureFlagManager) GetInt64Value(ctx context.Context, feature string, defaultValue int64, evalCtx featureflags.EvaluationContext) (int64, error) {
	ctx, op := f.o11y.Begin(ctx,
		observability.WithValue(keys.UserIDKey, evalCtx.TargetingKey),
		observability.WithValue("feature", feature),
	)
	defer op.End()

	if !f.circuitBreaker.CanProceed() {
		return defaultValue, circuitbreaking.ErrCircuitBroken
	}

	startTime := time.Now()
	result, err := f.ofClient.IntValue(ctx, feature, defaultValue, toOpenFeatureContext(evalCtx))
	f.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	if err != nil {
		f.errorCounter.Add(ctx, 1)
		f.circuitBreaker.Failed()
		return defaultValue, op.Error(err, "checking feature flag int variation")
	}

	op.Set("flag.default", defaultValue).Set("flag.value", result)

	f.evalCounter.Add(ctx, 1)
	f.circuitBreaker.Succeeded()
	return result, nil
}

// GetFloat64Value returns the float64 value of a feature flag, falling back to
// defaultValue on error.
func (f *featureFlagManager) GetFloat64Value(ctx context.Context, feature string, defaultValue float64, evalCtx featureflags.EvaluationContext) (float64, error) {
	ctx, op := f.o11y.Begin(ctx,
		observability.WithValue(keys.UserIDKey, evalCtx.TargetingKey),
		observability.WithValue("feature", feature),
	)
	defer op.End()

	if !f.circuitBreaker.CanProceed() {
		return defaultValue, circuitbreaking.ErrCircuitBroken
	}

	startTime := time.Now()
	result, err := f.ofClient.FloatValue(ctx, feature, defaultValue, toOpenFeatureContext(evalCtx))
	f.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	if err != nil {
		f.errorCounter.Add(ctx, 1)
		f.circuitBreaker.Failed()
		return defaultValue, op.Error(err, "checking feature flag float variation")
	}

	op.Set("flag.default", defaultValue).Set("flag.value", result)

	f.evalCounter.Add(ctx, 1)
	f.circuitBreaker.Succeeded()
	return result, nil
}

// GetObjectValue returns the object (JSON) value of a feature flag, falling back
// to defaultValue on error.
func (f *featureFlagManager) GetObjectValue(ctx context.Context, feature string, defaultValue any, evalCtx featureflags.EvaluationContext) (any, error) {
	ctx, op := f.o11y.Begin(ctx,
		observability.WithValue(keys.UserIDKey, evalCtx.TargetingKey),
		observability.WithValue("feature", feature),
	)
	defer op.End()

	if !f.circuitBreaker.CanProceed() {
		return defaultValue, circuitbreaking.ErrCircuitBroken
	}

	startTime := time.Now()
	result, err := f.ofClient.ObjectValue(ctx, feature, defaultValue, toOpenFeatureContext(evalCtx))
	f.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	if err != nil {
		f.errorCounter.Add(ctx, 1)
		f.circuitBreaker.Failed()
		return defaultValue, op.Error(err, "checking feature flag object variation")
	}

	f.evalCounter.Add(ctx, 1)
	f.circuitBreaker.Succeeded()
	return result, nil
}

// Close closes the PostHog client and detaches it from OpenFeature's
// process-global provider registry.
//
// Each construction registers a uniquely-named provider in that registry, which
// has no removal API — so without the swap below, every reload cycle left
// another registration holding a reference to a client that had just been
// closed, and the process accumulated them until it exited. Replacing the
// registration with the no-op provider releases the client; the (small,
// clientless) map entry itself is not removable and is left behind.
func (f *featureFlagManager) Close() error {
	var errs []error

	if err := openfeature.SetNamedProvider(f.domain, openfeature.NoopProvider{}); err != nil {
		errs = append(errs, platformerrors.Wrap(err, "detaching OpenFeature provider"))
	}

	if err := f.posthogClient.Close(); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}
