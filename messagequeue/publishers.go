package messagequeue

import (
	"context"

	platformerrors "github.com/primandproper/platform-go/v9/errors"
)

type (
	// Publisher produces events onto a queue.
	Publisher interface {
		// Stop halts all publishing.
		Stop()
		// Publish writes a message onto a message queue.
		Publish(ctx context.Context, data any) error
		// PublishAsync writes a message onto a message queue, but logs any encountered errors instead of returning them.
		PublishAsync(ctx context.Context, data any)
	}

	// PublisherProvider is a function that provides a Publisher for a given topic.
	PublisherProvider interface {
		Close()
		Ping(ctx context.Context) error
		NewPublisher(ctx context.Context, topic string) (Publisher, error)
	}
)

var (
	// ErrEmptyTopicName is returned when a topic name is empty.
	ErrEmptyTopicName = platformerrors.New("empty topic name")

	// ErrConsumerAlreadyRegistered is returned when a second consumer is
	// requested for a topic a provider already has one for.
	//
	// Providers cache consumers by topic, and the cache used to win silently:
	// the second caller got the first caller's consumer, wired to the first
	// caller's handler, and their own handler was never invoked for any message.
	// Nothing failed and nothing logged — the messages simply went somewhere else.
	//
	// One consumer per topic per provider is the rule; a caller that wants two
	// behaviors for one topic multiplexes inside its own handler.
	ErrConsumerAlreadyRegistered = platformerrors.New("a consumer is already registered for this topic")
)
