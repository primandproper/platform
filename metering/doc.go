/*
Package metering counts what customers consume and enforces what they are allowed
to, durably enough to invoice from.

The capitalism package can charge. It cannot count. Metering is the missing half,
and the hard parts are all guarantees rather than opinions: counting without
losing events under concurrency, enforcing a limit cheaply on the read path, and
pushing usage to a billing provider so that a retry is not a second charge. None
of those are application decisions, and every SaaS hand-rolls all three.

# Why this is not ratelimiting

The ratelimiting package answers "how fast?" over a sliding window and then
forgets, which is the correct design for its question — a rate limiter that
remembered last Tuesday would be enforcing something nobody asked for.

Metering answers "how much, this billing period?" and must never forget, because
the answer becomes an invoice. Same shape, opposite durability requirement. A
plan will typically want both, configured independently: a hundred requests a
second and a million requests a month are different limits that fail at different
times for different reasons.

# The shape

	registry := metering.NewRegistry()

	if err := registry.RegisterMeter(metering.Meter{
	    Name:        "llm_tokens",
	    Unit:        "tokens",
	    Aggregation: metering.AggregationSum,
	    Period:      metering.PeriodMonth,
	}); err != nil {
	    return err
	}

	if err := registry.RegisterQuota(metering.Quota{
	    Meter:    "llm_tokens",
	    Limit:    5_000_000,
	    Behavior: metering.BehaviorAllowOverage,
	    Period:   metering.PeriodMonth,
	}); err != nil {
	    return err
	}

Then three components over one Store, each wanted by a different process:

A Recorder ingests usage. It is the write path, says nothing about limits, and is
all a deployment that meters for billing but enforces nothing ever needs.

An Enforcer answers quota questions, on the request path.

A Flusher posts accumulated usage to the billing provider, on a jobs.Scheduler
tick in a worker.

# Idempotent ingest

Usage.IdempotencyKey is required, not optional. Every ingest path this package has
can present the same usage twice — an HTTP client that retries on a timeout, a
queue that redelivers on a lost ack — and usage that can be double-counted is
usage that produces wrong invoices.

Choosing one: it must be stable across the retries of a single logical event and
distinct across different events. The identifier of the thing that caused the
usage is almost always right — a request ID, a completion ID, a message ID —
because it is stable by construction and the caller already has it. A timestamp,
a random value generated at the call site, or a hash of the quantity are all
wrong, in the three different ways that are each obvious in hindsight.

Dedupe is the primary key of the event ledger table, and this is a considered
departure from the issue that specified the package, which proposed reusing the
idempotency package. That package is cache-backed with a TTL measured in hours,
and it is the right tool for its own problem: making an HTTP handler safe to
retry, where the window is a client's retry budget. Here the window is a billing
period. A dedupe that expired after a day would let a queue's dead-letter
redelivery, or a batch replayed by hand after a bad deploy, be counted a second
time — three weeks after the fact, into a total nobody has invoiced yet. So the
mechanism is one the database enforces, for as long as the row is retained, which
FlusherConfig.EventRetention defaults to ninety days.

# Durable counting

Every accepted record is written to the event ledger and folded into a period
total in the same transaction. The fold happens in the UPDATE statement rather
than in a read-modify-write, so two recorders folding into the same period at the
same instant cannot lose one of the two.

Note what the cache is and is not. The issue that specified this package proposed
buffering increments in the cache and reconciling them into the durable store on a
cron tick, with the stated requirement that "losing a Redis instance must cost
accuracy for seconds, not money". Buffered increments cannot meet that
requirement — they make the cache authoritative for everything since the last
reconciliation, so losing it loses exactly the usage that had not yet been
reconciled, which is money. So the direction is reversed: the durable total is
always the source of truth, and the cache is a read-through copy of it with a TTL.
Losing the cache costs latency until it repopulates and nothing else.

# Check is fast and slightly stale; Consume is exact

	// on a cheap path — a cached read, no write
	decision, err := enforcer.Check(ctx, accountID, "api_requests", 1)

	// on an expensive one — locks the total, decides, records, in one transaction
	decision, err := enforcer.ConsumeUsage(ctx, metering.Usage{
	    Subject:        accountID,
	    Meter:          "llm_tokens",
	    Quantity:       tokens,
	    IdempotencyKey: completionID,
	})

Two methods rather than one, because gating a cheap read on a durable write is how
metering becomes the latency bottleneck of the system it was added to measure.
Callers pick per call site.

Check's staleness is bounded by the cache TTL, which is Meter.Staleness or
EnforcerConfig.Staleness — ten seconds by default. The worst case is explicit:
a subject sitting exactly at their limit can consume, unenforced, for up to the
staleness budget, which is bounded by whatever one subject can push through in ten
seconds. For an API request quota that is a rounding error. For a meter whose unit
is worth real money, set Staleness lower, or use Consume, which has no staleness
at all.

Consume's signature on the Enforcer interface generates its own idempotency key,
which makes a retried Consume count twice. That is unavoidable — the signature has
no key to be stable across retries — so any path that can retry should use
ConsumeUsage instead.

# Overage is a behavior, not an error

	metering.BehaviorBlock         // refuse past the limit
	metering.BehaviorWarn          // allow, record, and report the overage
	metering.BehaviorAllowOverage  // allow, record, and consider it normal

BehaviorAllowOverage is how most usage billing actually works: a limit is where
the price changes, not where the service stops. Decision.Overage carries the
excess, which is the quantity an overage price is applied to.

# Idempotent flush

	scheduler.Register(flusher.Job(jobs.MustCron("0,5,10,15,20,25,30,35,40,45,50,55 * * * *"), 10*time.Minute))

This is the single most expensive thing in the package to get wrong, so three
mechanisms hold it together.

Each post carries the delta since the last successful post, not the running total.
Providers aggregate the records inside a billing period, so posting a cumulative
total every five minutes for a month would invoice roughly nine thousand times the
right number.

Each post's idempotency key is derived from (subject, meter, period, sequence),
where the sequence is a counter stored beside the total and incremented only on a
successful post. A retry of the same post computes the same key and the provider
ignores it; the next genuine post computes a different one. FlushIdempotencyKey is
exported so an operator reconciling an invoice by hand can compute what a given
post's key would have been.

The settle is guarded on the sequence the flusher read. A flusher whose lease
lapsed mid-post cannot advance a sequence another flusher has already moved,
which is the one race that would put the same delta on the wire under two
different keys — and the one an idempotency key cannot undo after the fact.

# Plans are not modeled here

Which plan a subject is on, what it entitles them to, and when it changed are
questions the billing provider's product catalog already answers. Modeling them
here would duplicate that catalog in a second place that can disagree with it, so
the package models none of them and asks instead: a QuotaSource maps a subject to
a limit, and a ProviderMapper maps a subject and meter to the provider-side handle
usage posts against. Both are one-method interfaces with function adapters.

The default QuotaSource serves the Registry's static quotas to every subject,
which is right for a deployment with one set of limits and wrong the moment two
customers can buy different amounts.

# Dimensions describe; they do not enforce

Usage.Dimensions — model, region, endpoint — are stored against the event for
later analysis and are deliberately absent from the aggregate key and from
enforcement. A dimensioned quota needs a total per combination of dimension
values, so the number of rows to keep becomes the product of every dimension's
cardinality, and a dimension whose values come from user input has no bound. A
caller that needs per-model limits registers a meter per model, where the
cardinality is a decision somebody made on purpose.

# What is not implemented

AggregationUniqueCount — monthly active users, distinct seats — is named and
refused at registration. Every other aggregation folds a record into a single
integer as it arrives; that one has to remember which values it has already seen,
which is a set or a HyperLogLog per period rather than a column. Treating it as a
sum would look right on a dashboard and be wrong on an invoice, so it fails at
wiring time instead.

# Storage

The store is SQL, and this package ships the DDL for it (metering/migrations) for
Postgres, MySQL, and SQLite. Two tables: an append-only event ledger keyed by
idempotency key, and a totals table keyed by (subject, meter, period start).

The library owns the schema because the counting logic is inseparable from it —
the dedupe is a primary key, the concurrent fold is an UPDATE expression, and the
atomicity of Consume is a row lock. A repository interface over those would be an
interface over three SQL features, implemented once. Store is still an interface,
for an application whose schema conventions differ enough to be worth
reimplementing it against.
*/
package metering
