package kubectl

import (
	"context"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("valid config", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Namespace: "default"}
		must.NoError(t, cfg.ValidateWithContext(context.Background()))
	})

	T.Run("valid config with kubeconfig", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Namespace: "production", Kubeconfig: "/home/user/.kube/config"}
		must.NoError(t, cfg.ValidateWithContext(context.Background()))
	})

	T.Run("missing namespace", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{}
		err := cfg.ValidateWithContext(context.Background())
		must.Error(t, err)
		test.StrContains(t, err.Error(), "namespace")
	})
}
