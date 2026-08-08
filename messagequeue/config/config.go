package messagequeuecfg

import (
	"context"
	"strings"

	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/messagequeue"
	"github.com/primandproper/platform-go/v10/messagequeue/kafka"
	"github.com/primandproper/platform-go/v10/messagequeue/noop"
	"github.com/primandproper/platform-go/v10/messagequeue/pubsub"
	"github.com/primandproper/platform-go/v10/messagequeue/redis"
	"github.com/primandproper/platform-go/v10/messagequeue/sqs"

	ps "cloud.google.com/go/pubsub/v2"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderRedis is used to refer to redis.
	ProviderRedis provider = "redis"
	// ProviderSQS is used to refer to sqs.
	ProviderSQS provider = "sqs"
	// ProviderPubSub is used to refer to GCP Pub/Sub.
	ProviderPubSub provider = "pubsub"
	// ProviderKafka is used to refer to Kafka.
	ProviderKafka provider = "kafka"
	// ProviderNoop discards published messages and consumes nothing. It must be
	// selected deliberately — it is never what an unrecognized provider falls
	// back to.
	ProviderNoop provider = "noop"
)

// providers are every provider this package implements, for validation.
var providers = []any{
	string(ProviderRedis),
	string(ProviderSQS),
	string(ProviderPubSub),
	string(ProviderKafka),
	string(ProviderNoop),
}

var (
	ErrNilConfig = errors.New("nil config provided")
)

type (
	// provider is used to indicate what messaging provider we'll use.
	provider string

	// MessageQueueConfig is used to indicate how the messaging provider should be configured.
	MessageQueueConfig struct {
		_        struct{}      `json:"-"            yaml:"-"`
		Kafka    kafka.Config  `envPrefix:"KAFKA_"  json:"kafka,omitzero"     yaml:"kafka,omitempty"`
		Provider provider      `env:"PROVIDER"      json:"provider,omitempty" yaml:"provider,omitempty"`
		SQS      sqs.Config    `envPrefix:"SQS_"    json:"sqs,omitzero"       yaml:"sqs,omitempty"`
		PubSub   pubsub.Config `envPrefix:"PUBSUB_" json:"pubSub,omitzero"    yaml:"pubSub,omitempty"`
		Redis    redis.Config  `envPrefix:"REDIS_"  json:"redis,omitzero"     yaml:"redis,omitempty"`
	}

	// Config is used to indicate how the messaging provider should be configured.
	Config struct {
		_ struct{} `json:"-" yaml:"-"`

		Consumer  MessageQueueConfig `envPrefix:"CONSUMER_"  json:"consumer,omitzero"  yaml:"consumer,omitempty"`
		Publisher MessageQueueConfig `envPrefix:"PUBLISHER_" json:"publisher,omitzero" yaml:"publisher,omitempty"`
	}
)

var (
	_ validation.ValidatableWithContext = (*Config)(nil)
	_ validation.ValidatableWithContext = (*MessageQueueConfig)(nil)
)

// ValidateWithContext validates a MessageQueueConfig struct.
func (c *MessageQueueConfig) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, c,
		validation.Field(&c.Provider, validation.Required, validation.In(providers...)),
	)
}

// ValidateWithContext validates a Config struct.
func (c *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, c,
		validation.Field(&c.Consumer),
		validation.Field(&c.Publisher),
	)
}

func cleanString(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// NewConsumerProvider provides a ConsumerProvider.
func NewConsumerProvider(ctx context.Context, c *Config, opts ...Option) (messagequeue.ConsumerProvider, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	if c == nil {
		return nil, ErrNilConfig
	}

	switch cleanString(string(c.Consumer.Provider)) {
	case string(ProviderRedis):
		return redis.NewRedisConsumerProvider(c.Consumer.Redis, redis.WithLogger(logger), redis.WithTracerProvider(tracerProvider), redis.WithMetricsProvider(metricsProvider)), nil
	case string(ProviderSQS):
		return sqs.NewSQSConsumerProvider(ctx, c.Consumer.SQS, sqs.WithLogger(logger), sqs.WithTracerProvider(tracerProvider), sqs.WithMetricsProvider(metricsProvider))
	case string(ProviderKafka):
		return kafka.NewKafkaConsumerProvider(c.Consumer.Kafka, kafka.WithLogger(logger), kafka.WithTracerProvider(tracerProvider), kafka.WithMetricsProvider(metricsProvider)), nil
	case string(ProviderPubSub):
		client, err := ps.NewClientWithConfig(ctx, c.Consumer.PubSub.ProjectID, &ps.ClientConfig{
			EnableOpenTelemetryTracing: true,
		})
		if err != nil {
			return nil, errors.Wrap(err, "establishing PubSub client")
		}

		return pubsub.NewPubSubConsumerProvider(client, pubsub.WithLogger(logger), pubsub.WithTracerProvider(tracerProvider), pubsub.WithMetricsProvider(metricsProvider)), nil
	case string(ProviderNoop):
		return noop.NewConsumerProvider(), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "messagequeue consumer provider %q", c.Consumer.Provider)
	}
}

// NewPublisherProvider provides a PublisherProvider.
func NewPublisherProvider(ctx context.Context, c *Config, opts ...Option) (messagequeue.PublisherProvider, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	if c == nil {
		return nil, ErrNilConfig
	}

	switch cleanString(string(c.Publisher.Provider)) {
	case string(ProviderRedis):
		return redis.NewRedisPublisherProvider(c.Publisher.Redis, redis.WithLogger(logger), redis.WithTracerProvider(tracerProvider), redis.WithMetricsProvider(metricsProvider)), nil
	case string(ProviderSQS):
		return sqs.NewSQSPublisherProvider(ctx, c.Publisher.SQS, sqs.WithLogger(logger), sqs.WithTracerProvider(tracerProvider), sqs.WithMetricsProvider(metricsProvider))
	case string(ProviderKafka):
		return kafka.NewKafkaPublisherProvider(c.Publisher.Kafka, kafka.WithLogger(logger), kafka.WithTracerProvider(tracerProvider), kafka.WithMetricsProvider(metricsProvider)), nil
	case string(ProviderPubSub):
		client, err := ps.NewClientWithConfig(ctx, c.Publisher.PubSub.ProjectID, &ps.ClientConfig{
			EnableOpenTelemetryTracing: true,
		})
		if err != nil {
			return nil, errors.Wrap(err, "establishing PubSub client")
		}

		return pubsub.NewPubSubPublisherProvider(client, c.Publisher.PubSub.ProjectID, pubsub.WithLogger(logger), pubsub.WithTracerProvider(tracerProvider), pubsub.WithMetricsProvider(metricsProvider)), nil
	case string(ProviderNoop):
		return noop.NewPublisherProvider(), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "messagequeue publisher provider %q", c.Publisher.Provider)
	}
}
