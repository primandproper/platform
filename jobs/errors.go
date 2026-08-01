package jobs

import (
	platformerrors "github.com/primandproper/platform-go/v9/errors"
)

// Scheduler sentinels.
var (
	// ErrJobPanicked wraps the value recovered from a scheduled job that
	// panicked. The Scheduler contains the panic rather than letting it unwind
	// the job's goroutine, which would stop that job — and only that job —
	// silently for the life of the process.
	ErrJobPanicked = platformerrors.New("scheduled job panicked")
	// ErrNilLocker indicates a nil Locker was passed to NewScheduler. It wraps
	// errors.ErrNilInputParameter, so a caller may check either.
	ErrNilLocker = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil distributed locker")
	// ErrSchedulerRunning indicates Register was called after Run. The job set
	// is fixed at Run, because each job owns a goroutine started there.
	ErrSchedulerRunning = platformerrors.New("scheduler is already running")
	// ErrDuplicateJob indicates two jobs were registered under one name. Names
	// are the lock keys, so duplicates would contend with each other rather
	// than run independently.
	ErrDuplicateJob = platformerrors.New("duplicate job name")
	// ErrInvalidJob indicates a job with no name, no function, or no usable
	// cadence — neither a positive interval nor a schedule, both at once, or a
	// schedule that will never fire.
	ErrInvalidJob = platformerrors.New("invalid job")
)

// Pool sentinels.
var (
	// ErrHandlerPanicked wraps the value recovered from a handler that panicked.
	// The Pool contains the panic rather than letting it unwind the worker
	// goroutine and take the process with it, then treats it as an ordinary
	// attempt failure.
	ErrHandlerPanicked = platformerrors.New("job handler panicked")
	// ErrNilHandler indicates a nil Handler was passed to NewPool. It wraps
	// errors.ErrNilInputParameter, so a caller may check either.
	ErrNilHandler = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil job handler")
	// ErrNilConsumerProvider indicates a nil ConsumerProvider was passed to
	// NewPool. It wraps errors.ErrNilInputParameter, so a caller may check
	// either.
	ErrNilConsumerProvider = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil consumer provider")
	// ErrNilPublisherProvider indicates a nil PublisherProvider was passed to
	// NewTopicDeadLetter. It wraps errors.ErrNilInputParameter, so a caller may
	// check either.
	ErrNilPublisherProvider = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil publisher provider")
)

// ErrInvalidCronSpec indicates a cron expression that could not be parsed. It
// joins the parser's own error, whose message names the offending field, so the
// sentinel is checkable and the detail is still readable.
var ErrInvalidCronSpec = platformerrors.New("invalid cron spec")
