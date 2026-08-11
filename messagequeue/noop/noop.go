package noop

import (
	"context"

	"github.com/primandproper/platform-go/v10/messagequeue"
)

var (
	_ messagequeue.PublisherProvider = (*PublisherProvider)(nil)
	_ messagequeue.Publisher         = (*Publisher)(nil)
	_ messagequeue.ConsumerProvider  = (*ConsumerProvider)(nil)
	_ messagequeue.Consumer          = (*Consumer)(nil)
)

// PublisherProvider is the no-op messagequeue.PublisherProvider.
type PublisherProvider struct{}

// NewPublisherProvider returns a no-op PublisherProvider.
func NewPublisherProvider() *PublisherProvider {
	return &PublisherProvider{}
}

func (n *PublisherProvider) Close() {}

func (n *PublisherProvider) Ping(context.Context) error { return nil }

func (n *PublisherProvider) NewPublisher(context.Context, string) (messagequeue.Publisher, error) {
	return NewPublisher(), nil
}

// Publisher is the no-op messagequeue.Publisher.
type Publisher struct{}

// NewPublisher returns a no-op Publisher.
func NewPublisher() *Publisher {
	return &Publisher{}
}

func (n *Publisher) Stop() {}

func (n *Publisher) Publish(context.Context, any) error {
	return nil
}

func (n *Publisher) PublishAsync(context.Context, any) {}

// ConsumerProvider is the no-op messagequeue.ConsumerProvider.
type ConsumerProvider struct{}

// NewConsumerProvider returns a no-op ConsumerProvider.
func NewConsumerProvider() *ConsumerProvider {
	return &ConsumerProvider{}
}

func (n *ConsumerProvider) Close() {}

func (n *ConsumerProvider) NewConsumer(context.Context, string, messagequeue.ConsumerFunc) (messagequeue.Consumer, error) {
	return NewConsumer(), nil
}

// Consumer is the no-op messagequeue.Consumer.
type Consumer struct{}

// NewConsumer returns a no-op Consumer.
func NewConsumer() *Consumer {
	return &Consumer{}
}

func (n *Consumer) Consume(context.Context, chan<- error) {}
