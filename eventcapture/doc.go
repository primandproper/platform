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
(JSONL file in eventcapture/jsonl today; an object-store exporter
implements the same three methods). Sink errors are logged, never surfaced:
the request that produced the event has long since been answered.

Because nothing here fails loudly, the metrics are the only way to learn that
capture has broken. Pass WithMetricsProvider and watch eventcapture_sink_errors
(the sink is rejecting records), eventcapture_records_dropped (producers are
outrunning the flusher — raise WithBufferSize or lower WithFlushInterval), and
eventcapture_aggregation_overflow (a composition hit its key bound). Drops are
accumulated on the hot path with an atomic and reported to the instrument at
flush time, so Record itself never pays for an instrument call. Flushes are not
traced — a root span every few seconds, parented to nothing, is noise — but
Close is, since abandoning a drain at shutdown loses captured events.

Recorder.Run deliberately takes no context: tied to a server context it would
stop consuming before the server finished draining in-flight requests,
silently dropping their events. Instead the owner calls Close after the
server has shut down; Close drains the buffer, runs a final flush, and closes
the sink.

Aggregator folds events into per-(key, time-bucket) rollups for consumers
that want densities instead of (or alongside) raw events; the key and counter
types are the caller's. It is deliberately lock-free and must only be touched
from the flusher goroutine — compose it through WithObserver (fold each
event), WithOnFlush (emit completed buckets), and WithOverflowSource (report
observations discarded at the key bound), all three of which run there. See
ExampleNewRecorder_aggregation for the full composition.

Note that WithObserver names the per-event hook and has nothing to do with
observability.Observer, which the Recorder builds internally from the logger
and tracer provider it is given.
*/
package eventcapture
