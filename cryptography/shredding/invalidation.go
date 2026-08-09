package shredding

import (
	"context"
	"encoding/json"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/messagequeue"
)

// DefaultInvalidationTopic is the topic a shred is announced on when a
// deployment has no opinion about topic naming.
const DefaultInvalidationTopic = "shredding_invalidations"

// ErrNilPublisher indicates a nil messagequeue.Publisher.
var ErrNilPublisher = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil shredding invalidation publisher")

var _ Broadcaster = (*queueBroadcaster)(nil)

// queueBroadcaster announces shreds over a message queue.
type queueBroadcaster struct {
	publisher messagequeue.Publisher
}

// NewQueueBroadcaster builds a Broadcaster over a messagequeue topic.
//
// What travels is a subject type and a subject ID, in the clear, on whatever bus
// the publisher is wired to. That is an identifier rather than any of the data
// the key protects — but it is still a statement that this person was erased,
// arriving on a topic every replica subscribes to, so a deployment whose bus is
// less trusted than its database should know it is being sent.
//
// Delivery semantics are the provider's. The redis provider is at-most-once, so
// a replica that was restarting simply misses the message and falls back to the
// TTL, which is the bound this is an improvement on rather than a replacement
// for. The at-least-once providers may deliver twice, which is harmless:
// dropping a key that is already gone is what Invalidate does anyway.
func NewQueueBroadcaster(publisher messagequeue.Publisher) (Broadcaster, error) {
	if publisher == nil {
		return nil, ErrNilPublisher
	}

	return &queueBroadcaster{publisher: publisher}, nil
}

// Broadcast implements Broadcaster.
func (b *queueBroadcaster) Broadcast(ctx context.Context, subject Subject) error {
	if err := subject.validate(); err != nil {
		return err
	}

	return b.publisher.Publish(ctx, subject)
}

// InvalidationHandler adapts an Invalidator to a messagequeue consumer, for the
// subscribing half of the same topic:
//
//	consumer, err := consumers.NewConsumer(ctx, shredding.DefaultInvalidationTopic,
//	    shredding.InvalidationHandler(keys))
//
// A replica that does not run this consumer is not broken; its cached keys
// expire on the TTL like they would with no broadcaster at all. It is the
// deployment that runs the publisher and forgets the subscriber that gets the
// worst of both: a metric saying invalidations are being sent, and nothing
// acting on them.
func InvalidationHandler(invalidator Invalidator) messagequeue.ConsumerFunc {
	return func(_ context.Context, data []byte) error {
		var subject Subject
		if err := json.Unmarshal(data, &subject); err != nil {
			return platformerrors.Wrap(err, "decoding shredding invalidation")
		}

		if err := subject.validate(); err != nil {
			return platformerrors.Wrap(err, "decoding shredding invalidation")
		}

		if invalidator != nil {
			invalidator.Invalidate(subject)
		}

		return nil
	}
}
