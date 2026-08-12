/*
Package multisource fans analytics events out to one reporter per named source.

It is not itself a vendor integration, and it is not an analytics.EventReporter:
every method takes a source name first — TrackEvent, AddUser,
TrackAnonymousEvent — and dispatches to the reporter configured for it. A mobile
app, a web front end, and a server-side job can therefore report through one
object while each lands in its own destination.

# What choosing it commits a caller to

The map is fixed at construction and never mutated, so sources are a deployment
decision rather than a runtime one, and a source that was not configured is
ErrUnknownSource — carrying the sorted list of sources that were, because the
next question is always "then what did I configure?". The alternative,
substituting a noop, lasts the life of the process: every event for that source
goes nowhere and every call returns nil.

For the same reason NewMultiSourceEventReporterFromConfig fails the whole call
when any one source fails to build. Partial success here is a service that runs
with a hole in its analytics and no way to notice.

Every event gets a "source" property, so a destination that cannot separate
sources by credential can separate them by property. That is not incidental:
PostHog reporters are deduplicated by API key, so two sources naming the same
key share one client, one buffer, and one circuit breaker, and the property is
the only thing that tells their events apart downstream. Sources naming
different keys get their own client, and with it their own credentials and
breaker.

Close flushes every distinct reporter exactly once — the deduplication means two
sources can be the same reporter — closes the rest even after one fails, and
joins the errors. Shutdown is the same call under do.Shutdowner, so a container
teardown flushes buffered events rather than dropping them.
*/
package multisource
