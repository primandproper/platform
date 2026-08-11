package eventstreamcfg

import (
	"testing"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("SSE provider", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			Provider: ProviderSSE,
		}

		test.NoError(t, cfg.ValidateWithContext(ctx))
	})

	T.Run("WebSocket provider", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			Provider: ProviderWebSocket,
		}

		test.Error(t, cfg.ValidateWithContext(ctx), test.Sprintf("websocket provider requires websocket config"))
	})

	T.Run("invalid provider", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cfg := &Config{
			Provider: "invalid",
		}

		test.Error(t, cfg.ValidateWithContext(ctx))
	})
}

func TestNewEventStreamUpgrader(T *testing.T) {
	T.Parallel()

	T.Run("SSE", func(t *testing.T) {
		t.Parallel()

		upgrader, err := NewEventStreamUpgrader(
			t.Context(),
			&Config{
				Provider: ProviderSSE,
			},
			nil,
		)

		must.NoError(t, err)
		test.NotNil(t, upgrader)
	})

	T.Run("WebSocket", func(t *testing.T) {
		t.Parallel()

		upgrader, err := NewEventStreamUpgrader(
			t.Context(),
			&Config{
				Provider: ProviderWebSocket,
			},
			nil,
		)

		must.NoError(t, err)
		test.NotNil(t, upgrader)
	})

	T.Run("invalid provider", func(t *testing.T) {
		t.Parallel()

		_, err := NewEventStreamUpgrader(t.Context(), &Config{}, nil)

		test.ErrorIs(t, err, platformerrors.ErrUnknownProvider)
	})
}

func TestNewBidirectionalEventStreamUpgrader(T *testing.T) {
	T.Parallel()

	T.Run("SSE returns error", func(t *testing.T) {
		t.Parallel()

		_, err := NewBidirectionalEventStreamUpgrader(
			t.Context(),
			&Config{
				Provider: ProviderSSE,
			},
			nil,
		)

		test.Error(t, err)
		test.StrContains(t, err.Error(), "SSE does not support bidirectional")
	})

	T.Run("WebSocket", func(t *testing.T) {
		t.Parallel()

		upgrader, err := NewBidirectionalEventStreamUpgrader(
			t.Context(),
			&Config{
				Provider: ProviderWebSocket,
			},
			nil,
		)

		must.NoError(t, err)
		test.NotNil(t, upgrader)
	})

	T.Run("invalid provider", func(t *testing.T) {
		t.Parallel()

		_, err := NewBidirectionalEventStreamUpgrader(t.Context(), &Config{}, nil)

		test.ErrorIs(t, err, platformerrors.ErrUnknownProvider)
	})
}
