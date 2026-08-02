package objectstorage

import (
	"context"
	"testing"

	"github.com/caarlos0/env/v11"
	"github.com/shoenig/test/must"
)

// TestConfigValidatesAfterEnvParse covers the interaction between the
// `env:",init"` tags on the provider sub-configs and the Nil rules that say the
// sub-configs for providers you did not select must be unset.
//
// env parsing allocates all three, so before normalization every config that
// had been through env.Parse failed validation — for every provider, with no
// value the operator could set to avoid it.
func TestConfigValidatesAfterEnvParse(T *testing.T) {
	T.Parallel()

	for _, provider := range []string{MemoryProvider, S3Provider, GCPCloudStorageProvider} {
		T.Run(provider+" validates once env parsing has allocated the sub-configs", func(t *testing.T) {
			t.Parallel()

			cfg := &Config{Provider: provider, BucketName: "bucket"}
			must.NoError(t, env.Parse(cfg))

			// env.Parse allocated all three, which is what the ",init" tag is for.
			must.NotNil(t, cfg.FilesystemConfig)
			must.NotNil(t, cfg.R2Config)
			must.NotNil(t, cfg.BackblazeB2Config)

			must.NoError(t, cfg.ValidateWithContext(context.Background()))
		})
	}

	T.Run("a sub-config for an unselected provider is still refused", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider:         MemoryProvider,
			BucketName:       "bucket",
			FilesystemConfig: &FilesystemConfig{RootDirectory: "/uploads"},
		}

		must.Error(t, cfg.ValidateWithContext(context.Background()))
	})

	T.Run("the selected provider still has to be configured", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Provider: FilesystemProvider, BucketName: "bucket"}
		must.NoError(t, env.Parse(cfg))

		must.Error(t, cfg.ValidateWithContext(context.Background()))
	})
}
