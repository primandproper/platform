package msgconfig

import (
	"testing"

	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/messagequeue/kafka"
	"github.com/primandproper/platform-go/v9/messagequeue/pubsub"
	"github.com/primandproper/platform-go/v9/messagequeue/sqs"
	loggingnoop "github.com/primandproper/platform-go/v9/observability/logging/noop"
	tracingnoop "github.com/primandproper/platform-go/v9/observability/tracing/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func Test_cleanString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.NotEq(t, "", cleanString(t.Name()))
	})
}

func TestNewConsumerProvider(T *testing.T) {
	T.Parallel()

	T.Run("with nil config", func(t *testing.T) {
		t.Parallel()

		p, err := NewConsumerProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, nil)
		test.Nil(t, p)
		test.ErrorIs(t, err, ErrNilConfig)
	})

	T.Run("with redis provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Consumer: MessageQueueConfig{
				Provider: ProviderRedis,
			},
		}

		p, err := NewConsumerProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, cfg)
		test.NoError(t, err)
		test.NotNil(t, p)
	})

	T.Run("with SQS provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Consumer: MessageQueueConfig{
				Provider: ProviderSQS,
				SQS:      sqs.Config{},
			},
		}

		p, err := NewConsumerProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, cfg)
		test.NoError(t, err)
		test.NotNil(t, p)
	})

	T.Run("with kafka provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Consumer: MessageQueueConfig{
				Provider: ProviderKafka,
				Kafka:    kafka.Config{Brokers: []string{"localhost:9092"}},
			},
		}

		p, err := NewConsumerProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, cfg)
		test.NoError(t, err)
		test.NotNil(t, p)
	})

	T.Run("with pubsub provider and empty project ID", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Consumer: MessageQueueConfig{
				Provider: ProviderPubSub,
				PubSub:   pubsub.Config{},
			},
		}

		p, err := NewConsumerProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, cfg)
		test.Nil(t, p)
		test.Error(t, err)
	})

	T.Run("with the noop provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Consumer: MessageQueueConfig{Provider: ProviderNoop}}
		p, err := NewConsumerProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, cfg)
		test.NoError(t, err)
		test.NotNil(t, p)
	})

	// An unset or typo'd provider used to yield a noop consumer that read
	// nothing, which is invisible until someone notices a queue growing.
	T.Run("with unknown provider returns ErrUnknownProvider", func(t *testing.T) {
		t.Parallel()

		p, err := NewConsumerProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, &Config{})
		test.ErrorIs(t, err, errors.ErrUnknownProvider)
		test.Nil(t, p)
	})
}

// TestNewConsumerProvider_PubSubEmulator covers the pubsub success branch.
// It must not run in parallel because it relies on PUBSUB_EMULATOR_HOST.
func TestNewConsumerProvider_PubSubEmulator(t *testing.T) {
	t.Setenv("PUBSUB_EMULATOR_HOST", "127.0.0.1:0")

	cfg := &Config{
		Consumer: MessageQueueConfig{
			Provider: ProviderPubSub,
			PubSub:   pubsub.Config{ProjectID: "test-project"},
		},
	}

	p, err := NewConsumerProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, cfg)
	must.NoError(t, err)
	test.NotNil(t, p)
}

func TestNewPublisherProvider(T *testing.T) {
	T.Parallel()

	T.Run("with nil config", func(t *testing.T) {
		t.Parallel()

		p, err := NewPublisherProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, nil)
		test.Nil(t, p)
		test.ErrorIs(t, err, ErrNilConfig)
	})

	T.Run("with redis provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Publisher: MessageQueueConfig{
				Provider: ProviderRedis,
			},
		}

		p, err := NewPublisherProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, cfg)
		test.NoError(t, err)
		test.NotNil(t, p)
	})

	T.Run("with SQS provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Publisher: MessageQueueConfig{
				Provider: ProviderSQS,
				SQS:      sqs.Config{},
			},
		}

		p, err := NewPublisherProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, cfg)
		test.NoError(t, err)
		test.NotNil(t, p)
	})

	T.Run("with kafka provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Publisher: MessageQueueConfig{
				Provider: ProviderKafka,
				Kafka:    kafka.Config{Brokers: []string{"localhost:9092"}},
			},
		}

		p, err := NewPublisherProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, cfg)
		test.NoError(t, err)
		test.NotNil(t, p)
	})

	T.Run("with pubsub provider and empty project ID", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Publisher: MessageQueueConfig{
				Provider: ProviderPubSub,
				PubSub:   pubsub.Config{},
			},
		}

		p, err := NewPublisherProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, cfg)
		test.Nil(t, p)
		test.Error(t, err)
	})

	T.Run("with the noop provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Publisher: MessageQueueConfig{Provider: ProviderNoop}}
		p, err := NewPublisherProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, cfg)
		test.NoError(t, err)
		test.NotNil(t, p)
	})

	// An unset or typo'd provider used to yield a noop publisher that discarded
	// every message it was handed.
	T.Run("with unknown provider returns ErrUnknownProvider", func(t *testing.T) {
		t.Parallel()

		p, err := NewPublisherProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, &Config{})
		test.ErrorIs(t, err, errors.ErrUnknownProvider)
		test.Nil(t, p)
	})
}

// TestNewPublisherProvider_PubSubEmulator covers the pubsub success branch.
// It must not run in parallel because it relies on PUBSUB_EMULATOR_HOST.
func TestNewPublisherProvider_PubSubEmulator(t *testing.T) {
	t.Setenv("PUBSUB_EMULATOR_HOST", "127.0.0.1:0")

	cfg := &Config{
		Publisher: MessageQueueConfig{
			Provider: ProviderPubSub,
			PubSub:   pubsub.Config{ProjectID: "test-project"},
		},
	}

	p, err := NewPublisherProvider(t.Context(), loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), nil, cfg)
	must.NoError(t, err)
	test.NotNil(t, p)
}
