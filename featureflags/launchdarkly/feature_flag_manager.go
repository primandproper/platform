package launchdarkly

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/primandproper/platform-go/v10/circuitbreaking"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/featureflags"
	"github.com/primandproper/platform-go/v10/identifiers"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/keys"
	"github.com/primandproper/platform-go/v10/observability/metrics"

	ld "github.com/launchdarkly/go-server-sdk/v6"
	"github.com/launchdarkly/go-server-sdk/v6/ldcomponents"
	ofld "github.com/open-feature/go-sdk-contrib/providers/launchdarkly/pkg"
	"github.com/open-feature/go-sdk/openfeature"
)

const (
	serviceName  = "launchdarkly_feature_flag_manager"
	clientDomain = "launchdarkly_feature_flags"
)

var (
	ErrMissingHTTPClient = platformerrors.New("missing HTTP client")
	ErrNilConfig         = platformerrors.New("missing config")
	ErrMissingSDKKey     = platformerrors.New("missing SDK key")
)

type (
	// featureFlagManager implements the feature flag interface using OpenFeature.
	featureFlagManager struct {
		circuitBreaker  circuitbreaking.CircuitBreaker
		o11y            observability.Observer
		evalCounter     metrics.Int64Counter
		errorCounter    metrics.Int64Counter
		notFoundCounter metrics.Int64Counter
		latencyHist     metrics.Float64Histogram
		ldClient        *ld.LDClient
		ofClient        *openfeature.Client
		domain          string
	}
)

// NewFeatureFlagManager constructs a new featureFlagManager backed by OpenFeature.
func NewFeatureFlagManager(cfg *Config, httpClient *http.Client, circuitBreaker circuitbreaking.CircuitBreaker, opts ...Option) (featureflags.FeatureFlagManager, error) {
	if httpClient == nil {
		return nil, ErrMissingHTTPClient
	}

	if cfg == nil {
		return nil, ErrNilConfig
	}

	if cfg.SDKKey == "" {
		return nil, ErrMissingSDKKey
	}

	if cfg.InitTimeout == time.Duration(0) {
		cfg.InitTimeout = 5 * time.Second
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

	// Counted apart from errors because the remedies differ: a missing flag is
	// answered by creating the flag, an error by fixing the provider. A sustained
	// rise here usually means a flag name shipped in code that nobody has created.
	notFoundCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_flags_not_found", serviceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating flag-not-found counter")
	}

	latencyHist, err := mp.NewFloat64Histogram(fmt.Sprintf("%s_latency_ms", serviceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating latency histogram")
	}

	// Each manager binds its own OpenFeature domain so a second manager can't rebind
	// the domain (and thus the provider/client) out from under an existing one.
	domain := fmt.Sprintf("%s_%s", clientDomain, identifiers.New())

	ldConfig := ld.Config{
		HTTP: ldcomponents.HTTPConfiguration().HTTPClientFactory(func() *http.Client { return httpClient }),
	}

	for _, modifier := range o.configModifiers {
		ldConfig = modifier(ldConfig)
	}

	client, err := ld.MakeCustomClient(
		cfg.SDKKey,
		ldConfig,
		cfg.InitTimeout,
	)
	if err != nil {
		return nil, platformerrors.Wrap(err, "error initializing LaunchDarkly client")
	}

	provider := ofld.NewProvider(client)
	if err = openfeature.SetNamedProviderAndWait(domain, provider); err != nil {
		if closeErr := client.Close(); closeErr != nil {
			o11y.Logger().Error("error closing OpenFeatureFlag client", closeErr)
		}
		return nil, platformerrors.Wrap(err, "failed to set OpenFeature provider")
	}

	ofClient := openfeature.NewClient(domain)

	ffm := &featureFlagManager{
		o11y:            o11y,
		domain:          domain,
		circuitBreaker:  circuitBreaker,
		ldClient:        client,
		ofClient:        ofClient,
		evalCounter:     evalCounter,
		errorCounter:    errorCounter,
		notFoundCounter: notFoundCounter,
		latencyHist:     latencyHist,
	}

	return ffm, nil
}

// toOpenFeatureContext converts a featureflags.EvaluationContext into the SDK's
// own representation. It is the only place this provider crosses the boundary
// between the platform-owned type and the OpenFeature type.
func toOpenFeatureContext(evalCtx featureflags.EvaluationContext) openfeature.EvaluationContext {
	return openfeature.NewEvaluationContext(evalCtx.TargetingKey, evalCtx.Attributes)
}

// evaluationError classifies a failed evaluation into the error the caller sees and
// the verdict the circuit breaker hears.
//
// A flag the provider resolved as absent scores a success. The breaker exists to
// give a failing service breathing room, and answering "no such flag" is not what a
// failing service does — it is a correct negative answer. Counting it as a failure
// is what let a flag name shipped ahead of its flag open a breaker that every other
// flag in the process shares.
//
// Everything else is a failure the breaker should hear about. That includes the
// SDK's pre-evaluation short circuits, which return empty resolution details and so
// arrive here with an empty code: an unready or fatally broken provider is exactly
// what the breaker is for.
func (f *featureFlagManager) evaluationError(ctx context.Context, feature string, code openfeature.ErrorCode, err error) error {
	if code == openfeature.FlagNotFoundCode {
		f.notFoundCounter.Add(ctx, 1)
		f.circuitBreaker.Succeeded()

		return platformerrors.Wrapf(featureflags.ErrFlagNotFound, "feature flag %q", feature)
	}

	f.errorCounter.Add(ctx, 1)
	f.circuitBreaker.Failed()

	return err
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
	details, err := f.ofClient.BooleanValueDetails(ctx, feature, false, toOpenFeatureContext(evalCtx))
	f.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	if err != nil {
		return false, op.Error(f.evaluationError(ctx, feature, details.ErrorCode, err), "checking feature flag variation")
	}

	op.Set("flag.value", details.Value)

	f.evalCounter.Add(ctx, 1)
	f.circuitBreaker.Succeeded()
	return details.Value, nil
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
	details, err := f.ofClient.StringValueDetails(ctx, feature, defaultValue, toOpenFeatureContext(evalCtx))
	f.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	if err != nil {
		return defaultValue, op.Error(f.evaluationError(ctx, feature, details.ErrorCode, err), "checking feature flag string variation")
	}

	op.Set("flag.default", defaultValue).Set("flag.value", details.Value)

	f.evalCounter.Add(ctx, 1)
	f.circuitBreaker.Succeeded()
	return details.Value, nil
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
	details, err := f.ofClient.IntValueDetails(ctx, feature, defaultValue, toOpenFeatureContext(evalCtx))
	f.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	if err != nil {
		return defaultValue, op.Error(f.evaluationError(ctx, feature, details.ErrorCode, err), "checking feature flag int variation")
	}

	op.Set("flag.default", defaultValue).Set("flag.value", details.Value)

	f.evalCounter.Add(ctx, 1)
	f.circuitBreaker.Succeeded()
	return details.Value, nil
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
	details, err := f.ofClient.FloatValueDetails(ctx, feature, defaultValue, toOpenFeatureContext(evalCtx))
	f.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	if err != nil {
		return defaultValue, op.Error(f.evaluationError(ctx, feature, details.ErrorCode, err), "checking feature flag float variation")
	}

	op.Set("flag.default", defaultValue).Set("flag.value", details.Value)

	f.evalCounter.Add(ctx, 1)
	f.circuitBreaker.Succeeded()
	return details.Value, nil
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
	details, err := f.ofClient.ObjectValueDetails(ctx, feature, defaultValue, toOpenFeatureContext(evalCtx))
	f.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	if err != nil {
		return defaultValue, op.Error(f.evaluationError(ctx, feature, details.ErrorCode, err), "checking feature flag object variation")
	}

	f.evalCounter.Add(ctx, 1)
	f.circuitBreaker.Succeeded()
	return details.Value, nil
}

// Close closes the LaunchDarkly client and detaches it from OpenFeature's
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

	if err := f.ldClient.Close(); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}
