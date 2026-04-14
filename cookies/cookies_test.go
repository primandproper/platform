package cookies

import (
	"encoding/base64"
	"testing"

	tracingnoop "github.com/primandproper/platform/observability/tracing/noop"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testKey = "HEREISA32CHARSECRETWHICHISMADEUP"
)

func buildConfigForTest() *Config {
	return &Config{
		Base64EncodedHashKey:  base64.StdEncoding.EncodeToString([]byte(testKey)),
		Base64EncodedBlockKey: base64.StdEncoding.EncodeToString([]byte(testKey)),
	}
}

func TestNewCookieManager(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		m, err := NewCookieManager(buildConfigForTest(), tracingnoop.NewTracerProvider())
		assert.NoError(t, err)
		assert.NotNil(t, m)
	})
}

type example struct {
	Name string
}

func Test_manager_Encode(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		m, err := NewCookieManager(buildConfigForTest(), tracingnoop.NewTracerProvider())
		require.NoError(t, err)
		require.NotNil(t, m)

		actual, err := m.Encode(ctx, "test", &example{Name: t.Name()})
		require.NoError(t, err)
		assert.NotEmpty(t, actual)
	})
}

func Test_manager_Decode(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		m, err := NewCookieManager(buildConfigForTest(), tracingnoop.NewTracerProvider())
		require.NoError(t, err)
		require.NotNil(t, m)

		encoded, err := m.Encode(ctx, "test", &example{Name: t.Name()})
		require.NoError(t, err)
		assert.NotEmpty(t, encoded)

		var actual example
		require.NoError(t, m.Decode(ctx, "test", encoded, &actual))
		assert.Equal(t, actual.Name, t.Name())
	})
}
