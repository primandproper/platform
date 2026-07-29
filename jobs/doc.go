/*
Package jobs supplies the lifecycle around background work: a bounded pool of
workers consuming a queue, and a scheduler that runs periodic work once across a
fleet.

messagequeue publishes and subscribes. It has nothing to say about how many
messages you handle at once, what happens to one that keeps failing, or how to
keep three replicas from all running the nightly reconcile. Those are the parts
every service reimplements, and the parts it gets subtly wrong. Everything here
is composed from primitives that already exist in this module — messagequeue,
retry, distributedlock, clock — so the package is a shape, not a new dependency.

# Queue workers

Pool binds a messagequeue.Consumer to a Handler:

	pool, err := jobs.NewPool(ctx, &jobs.PoolConfig{
		Topic:       "orders",
		Concurrency: 16,
	}, consumerProvider, handleOrder, jobs.WithPoolDeadLetter(deadLetter))
	if err != nil {
		return err
	}

	go pool.Run()
	defer func() { _ = pool.Close(shutdownCtx) }()

A returned error retries with backoff from retry.Config; past MaxAttempts the
message is dead-lettered. An error wrapped with retry.Unretryable skips the
remaining attempts and goes straight there, which is what a payload that fails
to parse deserves — it will fail to parse three more times, and each of those
attempts is latency the healthy messages behind it spend waiting.

# What the pool trades away

A messagequeue.Consumer calls its handler serially, so concurrency is only
possible if the handler returns before the work is done. Pool's handler hands
the payload to a worker and returns nil immediately.

That moves ownership of the message from the transport to this package. For a
transport that acknowledges when its handler returns, the message is
acknowledged before it has been processed: a crash loses whatever is in flight,
and redelivery is not the safety net it would be for a serial consumer. The
retry and dead-letter paths are what replaces it, and Concurrency is the bound
on the exposure — the work channel is unbuffered, so the Pool holds at most
Concurrency messages, and the consumer blocks rather than reading ahead.

Close drains: the consumer is stopped first, the workers are retired only once
it has returned and can hand out no more, and each finishes what it is already
holding. This is the same lesson as outbox.Relay.Run and
eventcapture.Recorder.Run, and the reason Run takes no context — tied to a
server context it would stop mid-message the instant that context was canceled,
which is precisely the wrong moment.

A message the consumer reads after that point is rejected rather than dropped,
so the transport does not record it as handled.

# Dead letters

A message that exhausts its attempts is wrapped in a DeadLetter envelope — the
payload plus why it died — and handed to a DeadLetterFunc. NewTopicDeadLetter
publishes to a queue topic; anything else (a table, an object store, a pager)
is a function.

Without one the Pool has no terminal destination and drops the message, logging
at error level and incrementing jobs_pool_messages_dropped. That is a
defensible choice for a topic whose individual messages are worthless and a
silent data-loss bug for every other topic, so the counter is worth an alert
either way.

The envelope is not wire-compatible with the source topic: replaying means
reading a DeadLetter, decoding its Payload, and publishing those bytes back.
Wrapping is unavoidable because messagequeue.Publisher carries no header
channel — the same constraint the outbox package documents — and the failure is
the only reason the record exists, so the alternative of republishing the bare
payload discards the point of it.

# Periodic jobs

Scheduler runs registered jobs on an interval, each execution held under a
distributedlock lease:

	scheduler, err := jobs.NewScheduler(&jobs.SchedulerConfig{}, locker)
	if err != nil {
		return err
	}

	if err = scheduler.Register(jobs.Job{
		Name:     "reconcile-balances",
		Interval: 5 * time.Minute,
		LeaseTTL: 2 * time.Minute,
		Run:      reconcileBalances,
	}); err != nil {
		return err
	}

	go scheduler.Run()
	defer func() { _ = scheduler.Close(shutdownCtx) }()

Every replica ticks; the one that wins the lock runs the job and the rest skip.
A contended lock is therefore not an error — it is the mechanism working — and
is counted separately as jobs_scheduler_skipped.

Intervals, not cron expressions. An interval covers the work this package is
for and needs no parser; a job that must run at 03:00 local time is asking for
a calendar, which is a different problem with time zones and daylight saving in
it.

The lease is not renewed while a job runs, so LeaseTTL must comfortably exceed
the job's worst-case duration. When it does not, Release reports
ErrLockNotHeld, the run is counted in jobs_scheduler_leases_expired, and it is
logged at error level — because by then a second replica may have started the
same job. That counter is the one to alert on.

Ticks are not queued. A job that overruns its interval fires again as soon as it
finishes rather than accumulating a backlog it can never work off; the overrun
is counted. A failed run is not retried either — the next tick is the retry, and
a job that cannot wait one interval wants a shorter interval, not an inner retry
loop.

The Scheduler is in-process: a job's function runs on the replica that won the
lease. Enqueueing onto messagequeue and letting a Pool execute it is the more
robust arrangement and considerably more machinery; it is also still available,
because a Job.Run that publishes a message is three lines.

# Panics

A panicking handler is contained in both halves. In the Pool it becomes an
ordinary attempt failure wrapping ErrHandlerPanicked, retried like any other and
dead-lettered at the end; in the Scheduler it becomes a failed run wrapping
ErrJobPanicked. The stack is attached to the span, since the goroutine that
would have printed it is the one being rescued.

Uncontained, the Scheduler case is the worse of the two: it would unwind that
job's goroutine and stop that job — and only that job — for the life of the
process, which surfaces months later as "the nightly report stopped arriving"
rather than as a crash now.

# Watching it

Nothing here fails loudly. Both halves swallow errors by design, because there
is no caller to hand them to, so the instruments are how you learn background
work has stopped working. Pass the metrics provider.

For the Pool: jobs_pool_messages_received against jobs_pool_messages_processed
(the gap is what is failing), jobs_pool_attempts_failed,
jobs_pool_messages_dead_lettered and jobs_pool_messages_dropped — alert on
both, each increment is a message nobody handled — jobs_pool_handler_panics,
jobs_pool_dead_letter_failures, jobs_pool_consumer_errors, and the
jobs_pool_in_flight up-down counter. Everything carries a topic attribute.

Three distributions, and the difference between them is the point.
jobs_pool_handler_latency_ms is one attempt, so it says how expensive the
handler is. jobs_pool_message_latency_ms is the whole message including every
retry and the backoff between them, so the gap between the two is what
retrying costs. jobs_pool_queue_wait_ms is how long a consumed message sat
before a worker took it, which is the backpressure signal: a rising p99 there
means Concurrency is too low, well before throughput visibly drops.

Spans cover each message, each attempt within it, and each dead-letter write.
Attempts are nested rather than merged so a slow attempt is attributable to the
attempt, and the dead-letter write is separate because a broker that has gone
down looks exactly like a handler that has, otherwise.

The message span carries a link back to the consume that produced it. A link
rather than a parent: the consumer ends its span as soon as the Pool's handler
returns, which is before the work starts, so a child would outlive its parent
and both durations would be lies. The link says what is true — this was caused
by that — and survives the goroutine hop the worker pool depends on.

For the Scheduler: jobs_scheduler_runs, jobs_scheduler_failures,
jobs_scheduler_skipped, jobs_scheduler_panics, jobs_scheduler_lock_errors,
jobs_scheduler_leases_expired, jobs_scheduler_overruns, and
jobs_scheduler_run_latency_ms, each carrying the job name. Runs and skips
together are the fleet's tick count; runs alone are what actually happened.

A tick is traced whether or not it ran, and carries jobs.ran to say which —
"did this replica decline, or did nobody run it" is the question a missed job
actually raises. The run nests inside the tick, so a slow job and a slow lock
backend are distinguishable; they call for opposite responses.

# Non-goals

This is not a distributed task queue with its own storage — no job arguments
persisted, no workflow state, no scheduled-for-later. That is Temporal and
River territory. This wraps the transports already here.
*/
package jobs
