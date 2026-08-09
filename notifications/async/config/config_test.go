package asynccfg

import (
	"fmt"
	"testing"

	"github.com/primandproper/platform-go/v10/notifications/async/ably"
	"github.com/primandproper/platform-go/v10/notifications/async/pusher"
	asyncws "github.com/primandproper/platform-go/v10/notifications/async/websocket"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: ProviderNoop,
		}

		must.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("with invalid provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: "invalid",
		}

		must.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("pusher requires config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: ProviderPusher,
		}

		must.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("ably requires config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: ProviderAbly,
		}

		must.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("self-hosted providers require a topology declaration", func(t *testing.T) {
		t.Parallel()

		// The #113 failure: a config that looks complete, loads without
		// complaint, and silently drops notifications the moment a second
		// replica exists. Nothing here can count replicas, so the only way to
		// tell the correct deployment from the broken one is to make the
		// operator say which it is.
		for _, provider := range []string{ProviderSSE, ProviderWebSocket} {
			cfg := &Config{Provider: provider, WebSocket: &asyncws.Config{}}

			test.ErrorIs(t, cfg.ValidateWithContext(t.Context()), ErrTopologyRequired)

			_, err := cfg.NewAsyncNotifier(nil)
			test.ErrorIs(t, err, ErrTopologyRequired)
		}
	})

	T.Run("self-hosted providers refuse a fleet", func(t *testing.T) {
		t.Parallel()

		for _, provider := range []string{ProviderSSE, ProviderWebSocket} {
			cfg := &Config{Provider: provider, WebSocket: &asyncws.Config{}, Topology: TopologyFleet}

			test.ErrorIs(t, cfg.ValidateWithContext(t.Context()), ErrFleetUnsupportedForSelfHostedProvider)

			_, err := cfg.NewAsyncNotifier(nil)
			test.ErrorIs(t, err, ErrFleetUnsupportedForSelfHostedProvider)
		}
	})

	T.Run("hosted and noop providers ignore topology", func(t *testing.T) {
		t.Parallel()

		// A hosted broker holds the connections, so replica count is not
		// load-bearing and an undeclared topology is not a mistake.
		for _, topology := range []string{"", TopologySingleReplica, TopologyFleet} {
			cfg := &Config{Provider: ProviderNoop, Topology: topology}
			must.NoError(t, cfg.ValidateWithContext(t.Context()))

			actual, err := cfg.NewAsyncNotifier(nil)
			test.NotNil(t, actual)
			test.NoError(t, err)
		}
	})

	T.Run("with an unrecognized topology", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Provider: ProviderSSE, Topology: "two_ish"}

		must.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("websocket requires config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: ProviderWebSocket,
		}

		must.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestConfig_NewAsyncNotifier(T *testing.T) {
	T.Parallel()

	T.Run("with websocket", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider:  ProviderWebSocket,
			WebSocket: &asyncws.Config{},
			Topology:  TopologySingleReplica,
		}

		actual, err := cfg.NewAsyncNotifier(nil)
		test.NotNil(t, actual)
		test.NoError(t, err)
	})

	T.Run("with sse", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: ProviderSSE,
			Topology: TopologySingleReplica,
		}

		actual, err := cfg.NewAsyncNotifier(nil)
		test.NotNil(t, actual)
		test.NoError(t, err)
	})

	T.Run("with pusher", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: ProviderPusher,
			Pusher: &pusher.Config{
				AppID:   "123",
				Key:     "key",
				Secret:  "secret",
				Cluster: "us2",
			},
		}

		actual, err := cfg.NewAsyncNotifier(nil)
		test.NotNil(t, actual)
		test.NoError(t, err)
	})

	T.Run("with ably", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: ProviderAbly,
			Ably: &ably.Config{
				APIKey: "appid.keyid:keysecret",
			},
		}

		actual, err := cfg.NewAsyncNotifier(nil)
		test.NotNil(t, actual)
		test.NoError(t, err)
	})

	noopProviders := []string{ProviderNoop}
	for _, provider := range noopProviders {
		T.Run(fmt.Sprintf("with noop provider %q", provider), func(t *testing.T) {
			t.Parallel()

			cfg := &Config{
				Provider: provider,
			}

			actual, err := cfg.NewAsyncNotifier(nil)
			test.NotNil(t, actual)
			test.NoError(t, err)
		})
	}

	T.Run("with unknown provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: "unknown",
		}

		actual, err := cfg.NewAsyncNotifier(nil)
		test.Nil(t, actual)
		test.Error(t, err)
	})
}

func TestNewAsyncNotifierFromConfig(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: ProviderNoop,
		}

		actual, err := NewAsyncNotifier(t.Context(), cfg, nil)
		test.NoError(t, err)
		test.NotNil(t, actual)
	})

	T.Run("with unknown provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider: "unknown",
		}

		actual, err := NewAsyncNotifier(t.Context(), cfg, nil)
		test.Nil(t, actual)
		test.Error(t, err)
	})
}
