/*
Package jobs supplies the lifecycle around background work: a bounded pool of
workers consuming a queue, and a scheduler that runs periodic work once across a
fleet.

messagequeue publishes and subscribes. It has nothing to say about how many
messages you handle at once, what happens to one that keeps failing, or how to
keep three replicas from all running the nightly reconcile. Those are the parts
every service reimplements, and the parts it gets subtly wrong. Nearly all of it
is composed from primitives that already exist in this module — messagequeue,
retry, distributedlock, clock — so the package is mostly a shape rather than
machinery. The one thing it brings from outside is robfig/cron's expression
parser, which the Scheduler's calendar schedules are built on.

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
retry and dead-letter paths are what replaces it, and Concurrency is what bounds
the exposure — the work channel is unbuffered, so the consumer blocks rather
than reading ahead. Precisely, a crash can lose Concurrency+1 messages: one per
busy worker, plus the one the consumer is blocked handing over. The point is
that the bound is a small constant you choose, not the transport's prefetch.

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

	scheduler, err := jobs.NewScheduler(ctx, &jobs.SchedulerConfig{}, locker)
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

# Calendars

A job that belongs at an hour rather than at a frequency sets Schedule instead
of Interval — exactly one of the two, and a job carrying both is rejected at
Register rather than resolved by precedence:

	if err = scheduler.Register(jobs.Job{
		Name:     "compact-audit-log",
		Schedule: jobs.MustCron("0 3 * * *"),
		LeaseTTL: 30 * time.Minute,
		Run:      compactAuditLog,
	}); err != nil {
		return err
	}

Cron takes a standard five-field crontab expression and the usual descriptors
(@daily, @every 30m). Schedule is an interface with one method, so a schedule
read from a table, one that stops after a date, or one that skips holidays is a
Next away — and any robfig/cron Schedule satisfies it as-is, for a caller that
wants seconds-resolution specs or the non-standard field extensions.

The two shapes differ in more than notation. An interval job's ticker is phased
by whenever its replica started, so a fleet's ticks are scattered across the
interval and the lease has to cover that scatter to keep the job from running
more than once per period. Every replica computes the same fire times from the
same expression, so a calendar job's fleet contends at the same instant and the
lease only has to cover the run plus the clock skew between replicas.

# Which zone a calendar job runs in

Four things can decide it, and the most specific wins. In order: a CRON_TZ= or
TZ= prefix on the expression itself; the location passed to CronIn; the
Scheduler's SchedulerConfig.Timezone; UTC.

	scheduler, err := jobs.NewScheduler(ctx, &jobs.SchedulerConfig{
		Timezone: "America/Chicago",
	}, locker)

	// 03:00 in Chicago, from the config.
	jobs.MustCron("0 3 * * *")
	// 03:00 in Tokyo: the expression said so, and that is more specific.
	jobs.MustCron("CRON_TZ=Asia/Tokyo 0 3 * * *")
	// 03:00 in Tokyo, for a caller that already holds the location.
	jobs.MustCronIn(tokyo, "0 3 * * *")

SchedulerConfig.Timezone is the level a whole service sets once, instead of
prefixing twelve expressions: it reaches every cron schedule registered with
that Scheduler that did not settle the question itself. CronIn is for a caller
holding a *time.Location rather than a name — one loaded once at startup, or a
time.FixedZone, which has no name to write into an expression at all.

This is crontab's own arrangement — a file sets a default, a line overrides it —
and the levels resolve at Register, so one schedule value shared by two
Schedulers reads correctly in both.

UTC at the bottom rather than the host's local time, because the underlying
parser defaults to time.Local and Go builds time.Local from the process's TZ
environment variable. That would make one expression mean different instants on
a laptop and in a container, and let one replica disagree with another about
when 03:00 is, with nothing anywhere saying so. Nothing here reads TZ.

Any zone but UTC costs the two days a year that a local wall-clock time is not a
function of the calendar:

  - On the spring-forward day an hour does not exist, and a job scheduled inside
    it does not run at all that day.
  - On the fall-back day an hour happens twice, and a job scheduled inside it
    runs twice, an hour apart. The lease does not prevent this — it was released
    an hour earlier — so a job that must not run twice a year wants UTC.

That is the problem this package spent v8.0 declining to have, and it is worth
being deliberate about which side of it a given job wants. Any zone but UTC also
needs the zoneinfo database at runtime, which scratch and distroless images do
not carry; `import _ "time/tzdata"` in main embeds it. A name that cannot be
loaded fails NewScheduler rather than the first fire.

A schedule reports the zone it will actually be read in — "CRON_TZ=UTC 0 3 * * *"
for an expression that named none — so the jobs.schedule span attribute answers
the question rather than raising it, and what it reports parses back to the same
fire times.

There is no catch-up. Fire times that pass while the process is down, or while
a previous run of the same job is still going, are skipped rather than queued —
the same coalescing the interval path gets from its ticker. RunOnStart is the
escape hatch for work a deploy must not skip.

A schedule that will never fire — "0 0 30 2 *" parses cleanly and never comes
true — is rejected at Register, because the alternative is a job nobody notices
never ran.

# Leases and overruns

The lease is not renewed while a job runs, so LeaseTTL must comfortably exceed
the job's worst-case duration. When it does not, Release reports
ErrLockNotHeld, the run is counted in jobs_scheduler_leases_expired, and it is
logged at error level — because by then a second replica may have started the
same job. That counter is the one to alert on.

Ticks are not queued. A job that overruns fires again as soon as it finishes
rather than accumulating a backlog it can never work off; the overrun is
counted. Overrunning means outlasting the headroom to the next fire, which for
an interval job is the interval and for a calendar job varies with the calendar
— a job at "0 9 * * 1-5" has three days of it on Friday night and one on Monday.

A failed run is not retried either — the next tick is the retry, and a job that
cannot wait one interval wants a shorter interval, not an inner retry loop.

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
actually raises. A calendar job's tick also carries jobs.schedule, the
expression as written, so the trace answers "when was this supposed to run"
without the reader deriving it from jobs.interval — which on those spans is the
headroom to the next fire rather than a fixed period. The run nests inside the
tick, so a slow job and a slow lock backend are distinguishable; they call for
opposite responses.

# Non-goals

This is not a distributed task queue with its own storage — no job arguments
persisted, no workflow state, no scheduled-for-later. That is Temporal and
River territory. This wraps the transports already here.
*/
package jobs
