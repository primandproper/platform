package cfgnorm_test

import (
	"context"
	"testing"

	analyticscfg "github.com/primandproper/platform-go/v9/analytics/config"
	authorizationcfg "github.com/primandproper/platform-go/v9/authorization/config"
	cachecfg "github.com/primandproper/platform-go/v9/cache/config"
	capitalismcfg "github.com/primandproper/platform-go/v9/capitalism/config"
	distributedlockcfg "github.com/primandproper/platform-go/v9/distributedlock/config"
	emailcfg "github.com/primandproper/platform-go/v9/email/config"
	embeddingscfg "github.com/primandproper/platform-go/v9/embeddings/config"
	featureflagscfg "github.com/primandproper/platform-go/v9/featureflags/config"
	llmcfg "github.com/primandproper/platform-go/v9/llm/config"
	asyncnotifcfg "github.com/primandproper/platform-go/v9/notifications/async/config"
	loggingcfg "github.com/primandproper/platform-go/v9/observability/logging/config"
	metricscfg "github.com/primandproper/platform-go/v9/observability/metrics/config"
	profilingcfg "github.com/primandproper/platform-go/v9/observability/profiling/config"
	tracingcfg "github.com/primandproper/platform-go/v9/observability/tracing/config"
	textsearchcfg "github.com/primandproper/platform-go/v9/search/text/config"
	vectorsearchcfg "github.com/primandproper/platform-go/v9/search/vector/config"
	secretscfg "github.com/primandproper/platform-go/v9/secrets/config"
	"github.com/primandproper/platform-go/v9/server/http"
	"github.com/primandproper/platform-go/v9/uploads/objectstorage"

	"github.com/caarlos0/env/v11"
	"github.com/shoenig/test/must"
)

// validatable is any config that validates itself.
type validatable interface {
	ValidateWithContext(context.Context) error
}

// TestSelectedProviderMustBeConfigured asserts that naming a provider and
// configuring nothing is refused, for every config whose provider sub-configs
// carry `env:",init"`.
//
// The tag makes env parsing allocate every provider's sub-config, so a
// `validation.Required` on the pointer stops meaning "the operator supplied
// one": a non-nil pointer to a zero struct satisfies Required. Each case here
// runs env.Parse first, which is what a real deployment does, so a rule that
// only holds before parsing does not count as holding.
//
// Anything that validates clean here boots with empty credentials.
//
// Two configs are deliberately absent, because for them a zero sub-config is a
// real configuration rather than an absent one, so this invariant does not apply
// as stated:
//
//   - eventstream's websocket config has no required field at all, so an empty
//     one is a fully defaulted one.
//   - notifications/mobile's FCM config documents an empty CredentialsPath as
//     "use Application Default Credentials", so an empty one is how ADC is
//     requested. Its "at least one of APNs or FCM" rule keys on nil-ness and no
//     longer holds after env parsing; restoring it needs a decision about what an
//     empty FCM block means, not a mechanical fix.
func TestSelectedProviderMustBeConfigured(T *testing.T) {
	T.Parallel()

	cases := []struct {
		cfg      validatable
		name     string
		provider string
	}{
		{name: "analytics/segment", provider: analyticscfg.ProviderSegment, cfg: &analyticscfg.Config{SourceConfig: analyticscfg.SourceConfig{Provider: analyticscfg.ProviderSegment}}},
		{name: "analytics/posthog", provider: analyticscfg.ProviderPostHog, cfg: &analyticscfg.Config{SourceConfig: analyticscfg.SourceConfig{Provider: analyticscfg.ProviderPostHog}}},
		{name: "authorization/database", provider: authorizationcfg.ProviderDatabase, cfg: &authorizationcfg.Config{Provider: authorizationcfg.ProviderDatabase}},
		{name: "cache/redis", provider: cachecfg.ProviderRedis, cfg: &cachecfg.Config{Provider: cachecfg.ProviderRedis}},
		{name: "capitalism/stripe", provider: capitalismcfg.StripeProvider, cfg: &capitalismcfg.Config{Provider: capitalismcfg.StripeProvider}},
		{name: "distributedlock/redis", provider: distributedlockcfg.RedisProvider, cfg: &distributedlockcfg.Config{Provider: distributedlockcfg.RedisProvider}},
		{name: "distributedlock/postgres", provider: distributedlockcfg.PostgresProvider, cfg: &distributedlockcfg.Config{Provider: distributedlockcfg.PostgresProvider}},
		{name: "email/sendgrid", provider: emailcfg.ProviderSendgrid, cfg: &emailcfg.Config{Provider: emailcfg.ProviderSendgrid}},
		{name: "email/mailgun", provider: emailcfg.ProviderMailgun, cfg: &emailcfg.Config{Provider: emailcfg.ProviderMailgun}},
		{name: "email/mailjet", provider: emailcfg.ProviderMailjet, cfg: &emailcfg.Config{Provider: emailcfg.ProviderMailjet}},
		{name: "email/resend", provider: emailcfg.ProviderResend, cfg: &emailcfg.Config{Provider: emailcfg.ProviderResend}},
		{name: "email/postmark", provider: emailcfg.ProviderPostmark, cfg: &emailcfg.Config{Provider: emailcfg.ProviderPostmark}},
		{name: "email/ses", provider: emailcfg.ProviderSES, cfg: &emailcfg.Config{Provider: emailcfg.ProviderSES}},
		{name: "embeddings/openai", provider: embeddingscfg.ProviderOpenAI, cfg: &embeddingscfg.Config{Provider: embeddingscfg.ProviderOpenAI}},
		{name: "embeddings/ollama", provider: embeddingscfg.ProviderOllama, cfg: &embeddingscfg.Config{Provider: embeddingscfg.ProviderOllama}},
		{name: "embeddings/cohere", provider: embeddingscfg.ProviderCohere, cfg: &embeddingscfg.Config{Provider: embeddingscfg.ProviderCohere}},
		{name: "featureflags/launchdarkly", provider: featureflagscfg.ProviderLaunchDarkly, cfg: &featureflagscfg.Config{Provider: featureflagscfg.ProviderLaunchDarkly}},
		{name: "featureflags/posthog", provider: featureflagscfg.ProviderPostHog, cfg: &featureflagscfg.Config{Provider: featureflagscfg.ProviderPostHog}},
		{name: "llm/openai", provider: llmcfg.ProviderOpenAI, cfg: &llmcfg.Config{Provider: llmcfg.ProviderOpenAI}},
		{name: "llm/anthropic", provider: llmcfg.ProviderAnthropic, cfg: &llmcfg.Config{Provider: llmcfg.ProviderAnthropic}},
		{name: "notifications-async/pusher", provider: asyncnotifcfg.ProviderPusher, cfg: &asyncnotifcfg.Config{Provider: asyncnotifcfg.ProviderPusher}},
		{name: "notifications-async/ably", provider: asyncnotifcfg.ProviderAbly, cfg: &asyncnotifcfg.Config{Provider: asyncnotifcfg.ProviderAbly}},
		{name: "logging/otelslog", provider: loggingcfg.ProviderOtelSlog, cfg: &loggingcfg.Config{Provider: loggingcfg.ProviderOtelSlog, ServiceName: "svc"}},
		{name: "metrics/otel", provider: metricscfg.ProviderOtel, cfg: &metricscfg.Config{Provider: metricscfg.ProviderOtel, ServiceName: "svc", Enabled: true}},
		{name: "profiling/pyroscope", provider: profilingcfg.ProviderPyroscope, cfg: &profilingcfg.Config{Provider: profilingcfg.ProviderPyroscope, ServiceName: "svc"}},
		{name: "tracing/otel", provider: tracingcfg.ProviderOtel, cfg: &tracingcfg.Config{Provider: tracingcfg.ProviderOtel, ServiceName: "svc"}},
		{name: "tracing/cloudtrace", provider: tracingcfg.ProviderCloudTrace, cfg: &tracingcfg.Config{Provider: tracingcfg.ProviderCloudTrace, ServiceName: "svc"}},
		{name: "search-text/algolia", provider: textsearchcfg.AlgoliaProvider, cfg: &textsearchcfg.Config{Provider: textsearchcfg.AlgoliaProvider}},
		{name: "search-text/elasticsearch", provider: textsearchcfg.ElasticsearchProvider, cfg: &textsearchcfg.Config{Provider: textsearchcfg.ElasticsearchProvider}},
		{name: "search-vector/pgvector", provider: vectorsearchcfg.PGvectorProvider, cfg: &vectorsearchcfg.Config{Provider: vectorsearchcfg.PGvectorProvider}},
		{name: "search-vector/qdrant", provider: vectorsearchcfg.QdrantProvider, cfg: &vectorsearchcfg.Config{Provider: vectorsearchcfg.QdrantProvider}},
		{name: "secrets/gcp", provider: secretscfg.ProviderGCP, cfg: &secretscfg.Config{Provider: secretscfg.ProviderGCP}},
		{name: "secrets/ssm", provider: secretscfg.ProviderSSM, cfg: &secretscfg.Config{Provider: secretscfg.ProviderSSM}},
		{name: "secrets/kubernetes", provider: secretscfg.ProviderKubernetes, cfg: &secretscfg.Config{Provider: secretscfg.ProviderKubernetes}},
		{name: "uploads/filesystem", provider: objectstorage.FilesystemProvider, cfg: &objectstorage.Config{Provider: objectstorage.FilesystemProvider, BucketName: "b"}},
		{name: "uploads/r2", provider: objectstorage.R2Provider, cfg: &objectstorage.Config{Provider: objectstorage.R2Provider, BucketName: "b"}},
		{name: "uploads/backblaze", provider: objectstorage.BackblazeB2Provider, cfg: &objectstorage.Config{Provider: objectstorage.BackblazeB2Provider, BucketName: "b"}},
	}

	for _, tc := range cases {
		T.Run(tc.name+" is refused when nothing is configured", func(t *testing.T) {
			t.Parallel()

			must.NoError(t, env.Parse(tc.cfg))
			must.Error(t, tc.cfg.ValidateWithContext(context.Background()),
				must.Sprintf("%s: naming provider %q and configuring nothing validated clean", tc.name, tc.provider))
		})
	}
}

// TestUnconfiguredOptionalsStayOff covers the other direction: a config that
// names no provider, or an optional feature nobody filled in, must not be
// switched on by env parsing having allocated its sub-config.
func TestUnconfiguredOptionalsStayOff(T *testing.T) {
	T.Parallel()

	T.Run("an unfilled apple-app-site-association does not serve the document", func(t *testing.T) {
		t.Parallel()

		cfg := &http.Config{}
		must.NoError(t, env.Parse(cfg))

		must.NotNil(t, cfg.AppleAppSiteAssociation)
		must.False(t, cfg.AppleAppSiteAssociation.Enabled())
		must.NoError(t, cfg.ValidateWithContext(context.Background()))
	})

	T.Run("profiling stays off when no provider is named", func(t *testing.T) {
		t.Parallel()

		cfg := &profilingcfg.Config{ServiceName: "svc"}
		must.NoError(t, env.Parse(cfg))
		must.NoError(t, cfg.ValidateWithContext(context.Background()))
	})
}
