/*
Package eventcapture records high-volume operational events for offline
analysis — model training data, usage matrices, replayable traces — without
ever slowing the request path that produces them. It is distinct from
analytics (low-volume product events to PostHog/Segment) and heavier than
logging: the write path here is designed for one event per served request.
The package is generic over the caller's event type; nothing about the
event's shape or meaning is prescribed.

The contract that shapes everything: capture must never block or fail the hot
path. Record is a non-blocking bounded-channel send — a full buffer drops the
event and counts the drop rather than waiting — and a single flusher
goroutine consumes the channel, writing records through a pluggable Sink
(JSONL file in eventcapture/jsonl today; a Kafka or object-store exporter
implements the same three methods). Sink errors are logged, never surfaced:
the request that produced the event has long since been answered.

Recorder.Run deliberately takes no context: tied to a server context it would
stop consuming before the server finished draining in-flight requests,
silently dropping their events. Instead the owner calls Close after the
server has shut down; Close drains the buffer, runs a final flush, and closes
the sink.

Aggregator folds events into per-(key, time-bucket) rollups for consumers
that want densities instead of (or alongside) raw events; the key and counter
types are the caller's. It is deliberately lock-free and must only be touched
from the flusher goroutine — compose it through WithObserver (fold each
event) and WithOnFlush (emit completed buckets), which both run there:

	agg := eventcapture.NewAggregator[Key, Counts](time.Minute, 10_000)
	rec := eventcapture.NewRecorder[Event](sink,
		eventcapture.WithObserver[Event](func(ev *Event) {
			agg.Observe(ev.Key, ev.At, func(c *Counts) { c.fold(ev) })
		}),
		eventcapture.WithOnFlush[Event](func(now time.Time, final bool, emit func(any)) {
			for _, b := range agg.Flush(now, final) {
				emit(rollupRecord(b))
			}
		}),
	)
	go rec.Run()
	defer rec.Close(ctx)
*/
package eventcapture
