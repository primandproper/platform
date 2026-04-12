package algolia

import (
	"testing"
	"time"

	"github.com/shoenig/test"
)

func TestConfig(T *testing.T) {
	T.Parallel()

	T.Run("zero value", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		test.EqOp(t, "", cfg.AppID)
		test.EqOp(t, "", cfg.APIKey)
		test.EqOp(t, time.Duration(0), cfg.Timeout)
	})

	T.Run("with values", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			AppID:   "test-app-id",
			APIKey:  "test-api-key",
			Timeout: 5 * time.Second,
		}

		test.EqOp(t, "test-app-id", cfg.AppID)
		test.EqOp(t, "test-api-key", cfg.APIKey)
		test.EqOp(t, 5*time.Second, cfg.Timeout)
	})
}
