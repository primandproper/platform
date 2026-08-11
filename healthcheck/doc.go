/*
Package healthcheck provides health check monitoring for service components with status tracking and aggregation via a registry pattern.

A registry answers two audiences at once. The probe endpoint asks "should this
process be sent traffic", gets a yes or a no, and asks again a second later; an
operator asks "when did that start", and a sequence of yes/no answers to a
question nobody recorded cannot tell them. So the registry remembers what each
component reported last time and reports the changes — a log line and a counter
increment per transition, once, however many probes observe the same state
afterwards — alongside a gauge of how many components are down right now.

That is what the observability options are for, and they are worth setting. A
registry with none still answers the probe correctly and tells nobody: a service
flapping in and out of a load balancer's rotation leaves a trail of JSON bodies
read by whatever polled it and nothing in any of the three pillars.
*/
package healthcheck
