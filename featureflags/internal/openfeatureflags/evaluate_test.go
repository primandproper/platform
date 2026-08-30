package openfeatureflags

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/primandproper/platform-go/v13/featureflags"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/observability/metrics"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// verdicts records what the evaluator told the breaker, which is the fact the
// not-found classification turns on and which no return value reports.
type verdicts struct {
	succeeded int
	failed    int
	proceed   bool
}

func (v *verdicts) CanProceed() bool    { return v.proceed }
func (v *verdicts) CannotProceed() bool { return !v.proceed }
func (v *verdicts) Succeeded()          { v.succeeded++ }
func (v *verdicts) Failed()             { v.failed++ }

// codedProvider answers every evaluation with one resolution error, so a test
// can name the error code the SDK hands back and watch how it is classified.
type codedProvider struct {
	openfeature.NoopProvider
	err openfeature.ResolutionError
}

func (p codedProvider) Metadata() openfeature.Metadata {
	return openfeature.Metadata{Name: "coded"}
}

func (p codedProvider) detail() openfeature.ProviderResolutionDetail {
	return openfeature.ProviderResolutionDetail{ResolutionError: p.err, Reason: openfeature.ErrorReason}
}

func (p codedProvider) BooleanEvaluation(context.Context, string, bool, openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	return openfeature.BoolResolutionDetail{ProviderResolutionDetail: p.detail()}
}

func (p codedProvider) StringEvaluation(context.Context, string, string, openfeature.FlattenedContext) openfeature.StringResolutionDetail {
	return openfeature.StringResolutionDetail{ProviderResolutionDetail: p.detail()}
}

func (p codedProvider) FloatEvaluation(context.Context, string, float64, openfeature.FlattenedContext) openfeature.FloatResolutionDetail {
	return openfeature.FloatResolutionDetail{ProviderResolutionDetail: p.detail()}
}

func (p codedProvider) IntEvaluation(context.Context, string, int64, openfeature.FlattenedContext) openfeature.IntResolutionDetail {
	return openfeature.IntResolutionDetail{ProviderResolutionDetail: p.detail()}
}

func (p codedProvider) ObjectEvaluation(context.Context, string, any, openfeature.FlattenedContext) openfeature.InterfaceResolutionDetail {
	return openfeature.InterfaceResolutionDetail{ProviderResolutionDetail: p.detail()}
}

// evaluatorFor binds an Evaluator to provider under a fresh domain, handing
// back the breaker verdicts it records.
func evaluatorFor(t *testing.T, provider openfeature.FeatureProvider) (*Evaluator, *verdicts) {
	t.Helper()

	domain := "openfeatureflags_eval_" + identifiers.New()

	must.NoError(t, openfeature.SetNamedProviderAndWait(domain, provider))
	t.Cleanup(func() { _ = openfeature.SetNamedProvider(domain, openfeature.NoopProvider{}) })

	set, err := metrics.NewOperationSet(nil, "openfeatureflags_eval_test")
	must.NoError(t, err)

	notFound, err := metrics.EnsureMetricsProvider(nil).NewInt64Counter("flags_not_found")
	must.NoError(t, err)

	v := &verdicts{proceed: true}

	return &Evaluator{
		O11y:            observability.NewObserverForTest("openfeatureflags_eval_test"),
		Client:          openfeature.NewClient(domain),
		CircuitBreaker:  v,
		Domain:          domain,
		EvalCounter:     set.Requests,
		ErrorCounter:    set.Errors,
		NotFoundCounter: notFound,
		LatencyHist:     set.Latency,
	}, v
}

// sharedEvalCtx is the context every evaluation here is made under.
var sharedEvalCtx = featureflags.EvaluationContext{TargetingKey: "user-1", Attributes: map[string]any{"plan": "pro"}}

func TestEvaluator_AnsweredEvaluations(T *testing.T) {
	T.Parallel()

	T.Run("each getter returns what the provider resolved and scores a success", func(t *testing.T) {
		t.Parallel()

		// NoopProvider resolves every flag to the caller's default with no
		// error, which is the answered path: the value comes back, the
		// evaluation is counted, and the breaker hears a success.
		e, v := evaluatorFor(t, openfeature.NoopProvider{})

		allowed, err := e.CanUseFeature(t.Context(), "flag", sharedEvalCtx)
		test.NoError(t, err)
		test.False(t, allowed)

		str, err := e.GetStringValue(t.Context(), "flag", "fallback", sharedEvalCtx)
		test.NoError(t, err)
		test.EqOp(t, "fallback", str)

		i, err := e.GetInt64Value(t.Context(), "flag", 7, sharedEvalCtx)
		test.NoError(t, err)
		test.EqOp(t, int64(7), i)

		f, err := e.GetFloat64Value(t.Context(), "flag", 1.5, sharedEvalCtx)
		test.NoError(t, err)
		test.EqOp(t, 1.5, f)

		obj, err := e.GetObjectValue(t.Context(), "flag", "fallback", sharedEvalCtx)
		test.NoError(t, err)
		test.EqOp(t, "fallback", obj)

		test.EqOp(t, 5, v.succeeded)
		test.EqOp(t, 0, v.failed)
	})
}

func TestEvaluator_NotFound(T *testing.T) {
	T.Parallel()

	T.Run("a missing flag is ErrFlagNotFound and scores a success on the breaker", func(t *testing.T) {
		t.Parallel()

		// The regression this pins: counting a missing flag as a failure let a
		// flag name shipped ahead of its flag open a breaker every other flag in
		// the process shares. Answering "no such flag" is a correct negative
		// answer, not a failing service.
		e, v := evaluatorFor(t, codedProvider{err: openfeature.NewFlagNotFoundResolutionError("nope")})

		_, err := e.GetStringValue(t.Context(), "unshipped", "fallback", sharedEvalCtx)
		test.ErrorIs(t, err, featureflags.ErrFlagNotFound)

		test.EqOp(t, 1, v.succeeded)
		test.EqOp(t, 0, v.failed)
	})

	T.Run("the flag name is named in the error", func(t *testing.T) {
		t.Parallel()

		e, _ := evaluatorFor(t, codedProvider{err: openfeature.NewFlagNotFoundResolutionError("nope")})

		_, err := e.GetInt64Value(t.Context(), "unshipped", 3, sharedEvalCtx)
		must.Error(t, err)
		test.StrContains(t, err.Error(), `"unshipped"`)
	})

	T.Run("the caller still gets its default", func(t *testing.T) {
		t.Parallel()

		e, _ := evaluatorFor(t, codedProvider{err: openfeature.NewFlagNotFoundResolutionError("nope")})

		str, err := e.GetStringValue(t.Context(), "unshipped", "fallback", sharedEvalCtx)
		test.ErrorIs(t, err, featureflags.ErrFlagNotFound)
		test.EqOp(t, "fallback", str)

		f, err := e.GetFloat64Value(t.Context(), "unshipped", 2.5, sharedEvalCtx)
		test.ErrorIs(t, err, featureflags.ErrFlagNotFound)
		test.EqOp(t, 2.5, f)

		obj, err := e.GetObjectValue(t.Context(), "unshipped", "fallback", sharedEvalCtx)
		test.ErrorIs(t, err, featureflags.ErrFlagNotFound)
		test.EqOp(t, "fallback", obj)
	})
}

func TestEvaluator_FailedEvaluations(T *testing.T) {
	T.Parallel()

	for _, tc := range []struct {
		err  openfeature.ResolutionError
		name string
	}{
		{name: "a general error", err: openfeature.NewGeneralResolutionError("boom")},
		{name: "a provider that is not ready", err: openfeature.NewProviderNotReadyResolutionError("starting")},
		{name: "a fatally broken provider", err: openfeature.NewProviderFatalResolutionError("dead")},
		{name: "a type mismatch", err: openfeature.NewTypeMismatchResolutionError("wrong type")},
		{name: "an invalid context", err: openfeature.NewInvalidContextResolutionError("bad context")},
	} {
		T.Run(tc.name+" is a failure the breaker hears about", func(t *testing.T) {
			t.Parallel()

			// Everything that is not FLAG_NOT_FOUND is what the breaker exists
			// for, the SDK's pre-evaluation short circuits included.
			e, v := evaluatorFor(t, codedProvider{err: tc.err})

			_, err := e.GetStringValue(t.Context(), "flag", "fallback", sharedEvalCtx)
			must.Error(t, err)
			test.False(t, stderrors.Is(err, featureflags.ErrFlagNotFound))

			test.EqOp(t, 0, v.succeeded)
			test.EqOp(t, 1, v.failed)
		})
	}

	T.Run("a failure still returns the caller's default rather than a zero value", func(t *testing.T) {
		t.Parallel()

		e, _ := evaluatorFor(t, codedProvider{err: openfeature.NewGeneralResolutionError("boom")})

		str, err := e.GetStringValue(t.Context(), "flag", "fallback", sharedEvalCtx)
		must.Error(t, err)
		test.EqOp(t, "fallback", str)

		i, err := e.GetInt64Value(t.Context(), "flag", 7, sharedEvalCtx)
		must.Error(t, err)
		test.EqOp(t, int64(7), i)

		allowed, err := e.CanUseFeature(t.Context(), "flag", sharedEvalCtx)
		must.Error(t, err)
		test.False(t, allowed)
	})
}
