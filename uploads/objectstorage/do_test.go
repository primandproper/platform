package objectstorage

import (
	"testing"

	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/metrics"
	"github.com/primandproper/platform/observability/tracing"
	"github.com/primandproper/platform/uploads"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterUploadManager(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue(i, t.Context())
		do.ProvideValue(i, logging.NewNoopLogger())
		do.ProvideValue(i, tracing.NewNoopTracerProvider())
		do.ProvideValue[metrics.Provider](i, metrics.NewNoopMetricsProvider())
		do.ProvideValue(i, &Config{
			BucketName: t.Name(),
			Provider:   MemoryProvider,
		})

		RegisterUploadManager(i)

		uploader, err := do.Invoke[*Uploader](i)
		must.NoError(t, err)
		test.NotNil(t, uploader)

		uploadManager, err := do.Invoke[uploads.UploadManager](i)
		must.NoError(t, err)
		test.NotNil(t, uploadManager)
	})
}
