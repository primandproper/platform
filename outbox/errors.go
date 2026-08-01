package outbox

import (
	platformerrors "github.com/primandproper/platform-go/v9/errors"
)

var (
	// ErrEmptyTopic indicates a Message was enqueued without a topic.
	ErrEmptyTopic = platformerrors.New("empty outbox message topic")
	// ErrNilPayload indicates a Message was enqueued with no payload.
	ErrNilPayload = platformerrors.New("nil outbox message payload")
	// ErrNilExecutor indicates Enqueue was called without a query executor. It
	// wraps errors.ErrNilInputParameter, so a caller may check either.
	ErrNilExecutor = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil query executor")
	// ErrInvalidClaimMode indicates a claim mode that is unknown, or unsupported
	// by the configured dialect.
	ErrInvalidClaimMode = platformerrors.New("invalid outbox claim mode")
	// ErrNilDatabaseClient indicates a nil database.Client was passed to
	// NewRelay. It wraps errors.ErrNilInputParameter, so a caller may check
	// either.
	ErrNilDatabaseClient = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil database client")
	// ErrNilPublisherProvider indicates a nil PublisherProvider was passed to
	// NewRelay. It wraps errors.ErrNilInputParameter, so a caller may check
	// either.
	ErrNilPublisherProvider = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil publisher provider")
)
