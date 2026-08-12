package messagequeue

type (
	// PublishOptions is the resolved form of the options given to a single
	// Publish or PublishAsync call. Callers set it through the With functions;
	// Publisher implementations read it, after resolving their variadic through
	// NewPublishOptions.
	//
	// It is per-message rather than per-publisher on purpose: the ordering key
	// is a property of the entity a message is about, and one publisher serves
	// every entity on its topic.
	PublishOptions struct {
		// OrderingKey names the sequence this message belongs to. Messages
		// published with the same key are delivered in the order they were
		// published; messages with different keys have no order relative to
		// each other, which is what lets a broker spread the topic over
		// partitions, groups or shards and still keep each entity's history
		// intact. The usual key is the ID of whatever the message is about — an
		// account, an order, a device.
		//
		// The empty string means "no ordering requirement" and is the default.
		// It is not a key: every backend that honors ordering treats it as
		// "spread this one freely", not as a group that all unkeyed messages
		// share, because one shared group would serialize the whole topic.
		//
		// What each backend does with it:
		//
		//   - kafka sets it as the message key and partitions on a murmur2 hash
		//     of it, so one key is one partition and Kafka's per-partition order
		//     is the guarantee.
		//   - sqs sets it as MessageGroupId, which a FIFO queue requires on
		//     every message. See DeduplicationKey, which a FIFO queue also
		//     needs unless the queue deduplicates on content.
		//   - pubsub sets it as the message's OrderingKey.
		//   - redis ignores it. Redis pub/sub has no ordering concept to map it
		//     onto: there are no partitions, no groups, and no ordering
		//     guarantee beyond what one connection happens to deliver. A key
		//     given to a redis publisher changes nothing and is not an error.
		//
		// Ordering is a joint property of publisher and subscriber, and only the
		// publishing half is this package's to set. Kafka delivers a partition
		// in order to whichever consumer owns it; SQS FIFO and Pub/Sub both need
		// the queue or subscription provisioned for ordered delivery, which
		// happens outside this package.
		OrderingKey string

		// DeduplicationKey names this message's identity for provider-side
		// deduplication: a broker that sees the key twice within its
		// deduplication window delivers the message once.
		//
		// Only sqs honors it, as MessageDeduplicationId. A FIFO queue requires
		// either this or ContentBasedDeduplication enabled on the queue itself —
		// with neither, SendMessage fails rather than publishing — so a caller
		// publishing to a FIFO queue that hashes its own bodies can leave this
		// empty, and one publishing to a FIFO queue that does not must set it.
		//
		// kafka, pubsub and redis ignore it. None of them deduplicate on a
		// caller-supplied key: Kafka's idempotent producer deduplicates on its
		// own sequence numbers, and Pub/Sub and Redis do not deduplicate at all.
		// Handlers on those backends must be idempotent regardless; see the
		// idempotency package.
		DeduplicationKey string
	}

	// PublishOption adjusts a single Publish or PublishAsync call.
	PublishOption func(*PublishOptions)
)

// OrderingKeyAttribute is the span attribute a publisher records the ordering
// key under, when one was given. It lives here rather than in each broker
// because three copies of an attribute name is three chances for one of them to
// drift, and a trace that names the same thing two ways cannot be queried for
// it.
//
// It is not recorded as a metric attribute: an ordering key is usually an
// entity ID, and one time series per entity is a cardinality explosion.
const OrderingKeyAttribute = "ordering_key"

// NewPublishOptions resolves a Publish call's variadic into the options every
// Publisher implementation reads. Options apply in order, so the last one to
// set a field wins, and a nil option is skipped rather than panicking.
//
// It is exported because implementations of Publisher live outside this package
// — the four in messagequeue, and any a consumer writes — and resolving the
// variadic in each of them is the kind of thing that drifts.
func NewPublishOptions(opts ...PublishOption) *PublishOptions {
	o := &PublishOptions{}

	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	return o
}

// WithOrderingKey sets the sequence a message belongs to. See
// PublishOptions.OrderingKey for what each backend does with it.
func WithOrderingKey(key string) PublishOption {
	return func(o *PublishOptions) { o.OrderingKey = key }
}

// WithDeduplicationKey sets a message's identity for provider-side
// deduplication. See PublishOptions.DeduplicationKey; only sqs honors it.
func WithDeduplicationKey(key string) PublishOption {
	return func(o *PublishOptions) { o.DeduplicationKey = key }
}
