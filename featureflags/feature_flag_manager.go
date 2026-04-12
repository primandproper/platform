package featureflags

import (
	"context"
)

// EvaluationContext carries targeting information for a single flag evaluation.
// TargetingKey is the primary subject identifier — typically a user ID, but it can
// be any stable string a provider's targeting rules can match against. Attributes
// carry arbitrary additional signals (tenant, plan tier, country, beta cohort,
// region, etc.) that provider rules can target on.
//
// This type is intentionally repo-owned rather than aliasing the OpenFeature SDK's
// EvaluationContext: it keeps the openfeature import out of caller code, lets the
// noop and mock implementations satisfy the signature without importing openfeature,
// and leaves room to swap providers later. Each provider converts to its own
// representation internally.
type EvaluationContext struct {
	Attributes   map[string]any
	TargetingKey string
}

type (
	// FeatureFlagManager evaluates feature flags. Implementations must be safe for
	// concurrent use.
	FeatureFlagManager interface {
		// CanUseFeature evaluates a boolean flag. Returns false on error.
		CanUseFeature(ctx context.Context, feature string, evalCtx EvaluationContext) (bool, error)
		// GetStringValue evaluates a string-typed flag, returning defaultValue on error.
		GetStringValue(ctx context.Context, feature, defaultValue string, evalCtx EvaluationContext) (string, error)
		// GetInt64Value evaluates an int64-typed flag, returning defaultValue on error.
		GetInt64Value(ctx context.Context, feature string, defaultValue int64, evalCtx EvaluationContext) (int64, error)
		// GetFloat64Value evaluates a float64-typed flag, returning defaultValue on error.
		GetFloat64Value(ctx context.Context, feature string, defaultValue float64, evalCtx EvaluationContext) (float64, error)
		// GetObjectValue evaluates an object-typed (JSON) flag, returning defaultValue
		// on error. The concrete type of the returned value is provider-specific —
		// callers typically type-assert or json.Marshal it back into a known struct.
		GetObjectValue(ctx context.Context, feature string, defaultValue any, evalCtx EvaluationContext) (any, error)
		// Close releases any backend resources held by the FeatureFlagManager.
		Close() error
	}
)
