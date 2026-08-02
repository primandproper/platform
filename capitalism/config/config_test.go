package config

import (
	"testing"

	"github.com/primandproper/platform-go/v9/capitalism/stripe"
	loggingnoop "github.com/primandproper/platform-go/v9/observability/logging/noop"
	tracingnoop "github.com/primandproper/platform-go/v9/observability/tracing/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			Enabled:  true,
			Provider: StripeProvider,
			Stripe:   &stripe.Config{WebhookSecret: t.Name()},
		}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})

	T.Run("returns nil when not enabled", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			Enabled: false,
		}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})

	T.Run("with invalid config", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			Enabled:  true,
			Provider: StripeProvider,
		}

		test.Error(t, cfg.ValidateWithContext(ctx))
	})
}

func TestNewPaymentManager(T *testing.T) {
	T.Parallel()

	T.Run("with stripe provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Enabled:  true,
			Provider: StripeProvider,
			Stripe:   &stripe.Config{WebhookSecret: t.Name()},
		}

		pm, err := NewPaymentManager(t.Context(), cfg, loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil)
		must.NoError(t, err)
		test.NotNil(t, pm)
	})

	T.Run("disabled returns noop", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Enabled: false,
		}

		pm, err := NewPaymentManager(t.Context(), cfg, loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil)
		must.NoError(t, err)
		test.NotNil(t, pm)
	})

	T.Run("with unknown provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Enabled:  true,
			Provider: "unknown",
		}

		pm, err := NewPaymentManager(t.Context(), cfg, loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil)
		test.Nil(t, pm)
		test.Error(t, err)
	})
}

func TestNewUsageReporter(T *testing.T) {
	T.Parallel()

	T.Run("with stripe provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Enabled:  true,
			Provider: StripeProvider,
			Stripe:   &stripe.Config{APIKey: "sk_test_123", WebhookSecret: t.Name()},
		}

		reporter, err := NewUsageReporter(t.Context(), cfg, loggingnoop.NewLogger(), tracingnoop.NewTracerProvider())
		must.NoError(t, err)
		test.NotNil(t, reporter)
	})

	T.Run("requires an API key for stripe", func(t *testing.T) {
		t.Parallel()

		// There is no inbound path for usage reporting, so a reporter without a
		// key could do nothing at all.
		cfg := &Config{
			Enabled:  true,
			Provider: StripeProvider,
			Stripe:   &stripe.Config{WebhookSecret: t.Name()},
		}

		_, err := NewUsageReporter(t.Context(), cfg, loggingnoop.NewLogger(), tracingnoop.NewTracerProvider())
		test.Error(t, err)
	})

	T.Run("disabled returns noop", func(t *testing.T) {
		t.Parallel()

		// "Meter everything, bill nothing" is a supported deployment rather than
		// an error, which is why this yields the noop instead of refusing.
		reporter, err := NewUsageReporter(
			t.Context(), &Config{Enabled: false}, loggingnoop.NewLogger(), tracingnoop.NewTracerProvider())
		must.NoError(t, err)
		test.NotNil(t, reporter)
	})

	T.Run("with unknown provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Enabled: true, Provider: "unknown"}

		reporter, err := NewUsageReporter(t.Context(), cfg, loggingnoop.NewLogger(), tracingnoop.NewTracerProvider())
		test.Nil(t, reporter)
		test.Error(t, err)
	})
}
