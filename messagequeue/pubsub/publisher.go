package pubsub

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/primandproper/platform-go/v10/encoding"
	"github.com/primandproper/platform-go/v10/messagequeue"
	"github.com/primandproper/platform-go/v10/messagequeue/internal/mqmetrics"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/keys"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	"github.com/primandproper/platform-go/v10/observability/tracing"

	"cloud.google.com/go/pubsub/v2"
)

type (
	messagePublisher interface {
		Stop()
		Publish(context.Context, *pubsub.Message) *pubsub.PublishResult
	}

	pubSubPublisher struct {
		o11y        observability.Observer
		encoder     encoding.ClientEncoder
		publisher   messagePublisher
		instruments *mqmetrics.Publisher
	}
)

// buildPubSubPublisher provides a Pub/Sub-backed pubSubPublisher.
func buildPubSubPublisher(logger logging.Logger, pubsubClient *pubsub.Publisher, tracerProvider tracing.Provider, metricsProvider metrics.Provider, topic string) (*pubSubPublisher, error) {
	instruments, err := mqmetrics.NewPublisher(metricsProvider, topic)
	if err != nil {
		return nil, err
	}

	return &pubSubPublisher{
		encoder:     encoding.NewClientEncoder(encoding.ContentTypeJSON, encoding.WithLogger(logger), encoding.WithTracerProvider(tracerProvider)),
		o11y:        observability.NewObserverWithValues(fmt.Sprintf("%s_publisher", topic), logger, tracerProvider, map[string]any{keys.TopicKey: topic}),
		publisher:   pubsubClient,
		instruments: instruments,
	}, nil
}

// Stop calls Stop on the topic.
func (p *pubSubPublisher) Stop() {
	p.publisher.Stop()
}

var _ messagequeue.PublisherProvider = (*PublisherProvider)(nil)

// PublisherProvider is the Google Cloud Pub/Sub messagequeue.PublisherProvider implementation. It is
// exported, and returned by NewPubSubPublisherProvider, so a caller who has chosen
// Google Cloud Pub/Sub can depend on that choice rather than on the interface every
// broker shares.
type PublisherProvider struct {
	logger            logging.Logger
	publisherCache    map[string]messagequeue.Publisher
	pubsubClient      *pubsub.Client
	tracerProvider    tracing.Provider
	metricsProvider   metrics.Provider
	projectID         string
	publisherCacheHat sync.RWMutex
}

// NewPubSubPublisherProvider returns a PublisherProvider for a given address.
func NewPubSubPublisherProvider(client *pubsub.Client, projectID string, opts ...Option) *PublisherProvider {
	o := newOptions(opts)

	return &PublisherProvider{
		logger:          logging.EnsureLogger(o.logger),
		pubsubClient:    client,
		publisherCache:  map[string]messagequeue.Publisher{},
		tracerProvider:  o.tracerProvider,
		metricsProvider: o.metricsProvider,
		projectID:       projectID,
	}
}

// Ping is a no-op for GCP Pub/Sub (managed service).
func (p *PublisherProvider) Ping(context.Context) error { return nil }

// Close closes the connection topic.
func (p *PublisherProvider) Close() {
	if err := p.pubsubClient.Close(); err != nil {
		p.logger.Error("closing pubsub connection", err)
	}
}

// qualifyTopicName ensures the topic name is fully qualified (projects/{project}/topics/{topic}).
func (p *PublisherProvider) qualifyTopicName(topicName string) string {
	if strings.HasPrefix(topicName, "projects/") {
		return topicName
	}
	return fmt.Sprintf("projects/%s/topics/%s", p.projectID, topicName)
}

// NewPublisher returns a pubSubPublisher for a given topic.
func (p *PublisherProvider) NewPublisher(ctx context.Context, topicName string) (messagequeue.Publisher, error) {
	if topicName == "" {
		return nil, messagequeue.ErrEmptyTopicName
	}

	qualifiedName := p.qualifyTopicName(topicName)

	logger := logging.EnsureLogger(p.logger.Clone())

	p.publisherCacheHat.Lock()
	defer p.publisherCacheHat.Unlock()
	if cachedPub, ok := p.publisherCache[qualifiedName]; ok {
		return cachedPub, nil
	}

	// Use Publisher directly with the qualified topic name. This avoids needing
	// pubsub.topics.get (TopicAdminClient.GetTopic); pubsub.topics.publish is sufficient.
	publisher := p.pubsubClient.Publisher(qualifiedName)

	pub, err := buildPubSubPublisher(logger, publisher, p.tracerProvider, p.metricsProvider, qualifiedName)
	if err != nil {
		return nil, err
	}
	p.publisherCache[qualifiedName] = pub

	return pub, nil
}

func (p *pubSubPublisher) Publish(ctx context.Context, data any) error {
	ctx, op := p.o11y.Begin(ctx)
	defer op.End()

	startTime := time.Now()

	var b bytes.Buffer
	if err := p.encoder.Encode(ctx, &b, data); err != nil {
		p.instruments.Failed(ctx)
		return observability.PrepareError(err, op.Span(), "encoding topic message")
	}

	op.Set(keys.LengthKey, b.Len())

	msg := &pubsub.Message{Data: b.Bytes()}
	result := p.publisher.Publish(ctx, msg)

	<-result.Ready()

	// The Get method blocks until a server-generated ID or an error is returned for the published message.
	serverID, err := result.Get(ctx)
	if err != nil {
		p.instruments.Failed(ctx)
		return op.Error(err, "publishing pubsub message")
	}

	op.SpanOnly("message_id", serverID)

	p.instruments.Published(ctx, startTime)

	op.Logger().Debug("published message")

	return nil
}

func (p *pubSubPublisher) PublishAsync(ctx context.Context, data any) {
	if err := p.Publish(ctx, data); err != nil {
		p.o11y.Logger().Error("publishing message", err)
	}
}
