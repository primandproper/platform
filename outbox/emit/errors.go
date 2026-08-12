package outboxemit

import (
	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

var (
	// ErrNilEnqueuer indicates a nil Enqueuer was passed to NewEmitter. It
	// wraps errors.ErrNilInputParameter, so a caller may check either.
	ErrNilEnqueuer = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil outbox enqueuer")

	// ErrNilSideEffect indicates WithSideEffect was given a nil function. It
	// wraps errors.ErrNilInputParameter, so a caller may check either.
	//
	// Refused at construction rather than skipped at Emit. A registered side
	// effect that quietly does nothing is the forgotten side effect this
	// package exists to rule out, wearing a registration.
	ErrNilSideEffect = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil outbox side effect")

	// ErrEmptySideEffectName indicates WithSideEffect was given no name. The
	// name is what identifies the side effect on a span and in the error a
	// failing one returns, so an unnamed side effect fails anonymously inside
	// somebody's transaction. It wraps errors.ErrEmptyInputParameter, so a
	// caller may check either.
	ErrEmptySideEffectName = platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "empty outbox side effect name")

	// ErrDuplicateSideEffect indicates two side effects were registered under
	// one name, which would make each of them report as the other.
	ErrDuplicateSideEffect = platformerrors.New("duplicate outbox side effect name")

	// ErrSideEffectTypeMismatch indicates WithSideEffect was given a side
	// effect for a message type other than the Emitter's. Option carries no
	// type parameter, so the compiler cannot catch it; NewEmitter does.
	ErrSideEffectTypeMismatch = platformerrors.New("side effect type does not match emitter message type")

	// ErrEmptyDocumentID indicates WithIndexUpsert or WithIndexDelete was given
	// no document ID. There is nothing to index and nothing to order by, and an
	// event carrying neither is one the Syncer would dead-letter on arrival. It
	// wraps errors.ErrEmptyInputParameter, so a caller may check either.
	ErrEmptyDocumentID = platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "empty index document ID")

	// ErrEmptyOrderingKey indicates WithOrderingKey was given an empty key.
	// Unkeyed is the default and is reached by not passing the option at all;
	// passing it empty is a caller who believes their message is ordered and
	// whose ID came back blank. It wraps errors.ErrEmptyInputParameter, so a
	// caller may check either.
	ErrEmptyOrderingKey = platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "empty outbox ordering key")
)
