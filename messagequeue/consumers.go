package messagequeue

import (
	"context"
)

type (
	// Consumer reads messages off a queue and hands each to its handler.
	//
	// # Stopping
	//
	// Consume runs until ctx is done. There is no separate stop channel: every
	// implementation turned one into a context cancellation immediately, so it
	// was a second way to say the same thing — and a `chan bool` at that, which
	// is bidirectional, so nothing stopped a caller from receiving on it and
	// stealing the stop signal from the consumer.
	//
	// # Delivery semantics
	//
	// These differ by backend, and the difference is load-bearing rather than an
	// implementation detail a caller can ignore:
	//
	//   - redis is at-most-once. It is pub/sub: a message delivered while no
	//     consumer is running is gone, and a handler that fails does not get the
	//     message again. Do not use it for work that must not be lost.
	//   - sqs, pubsub and kafka are at-least-once. A handler must therefore be
	//     idempotent — see the idempotency package — because redelivery is normal
	//     operation, not an error case.
	//
	// Handler errors are reported on errs, and what happens next also differs:
	// kafka stops the consumer, because its commits are cumulative by offset and
	// committing past a failed message would lose it; the others log the failure
	// and continue with the next message.
	//
	// errs is send-only and must be drained. A consumer whose error channel is
	// not being read does not block forever — it also selects on ctx — but it
	// does discard errors while nobody is listening.
	Consumer interface {
		Consume(ctx context.Context, errs chan<- error)
	}

	// ConsumerFunc is a function type that handles consumed messages.
	ConsumerFunc func(context.Context, []byte) error

	// ConsumerProvider is a function that provides a Consumer for a given topic.
	//
	// One consumer per topic: a second NewConsumer for a topic that already has
	// one returns ErrConsumerAlreadyRegistered rather than silently handing back
	// the first caller's consumer, wired to the first caller's handler.
	ConsumerProvider interface {
		Close()
		NewConsumer(ctx context.Context, topic string, handlerFunc ConsumerFunc) (Consumer, error)
	}
)
