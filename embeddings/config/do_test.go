package embeddingscfg

import (
	"testing"

	"github.com/primandproper/platform/embeddings"
	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/tracing"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterEmbedder(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, logging.NewNoopLogger())
		do.ProvideValue(i, tracing.NewTracerForTest("test"))
		do.ProvideValue(i, &Config{})

		RegisterEmbedder(i)

		embedder, err := do.Invoke[embeddings.Embedder](i)
		must.NoError(t, err)
		test.NotNil(t, embedder)
	})
}
