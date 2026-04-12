/*
Package metrics provides a metrics-tracking implementation for the service.

What to use and when:

	Use Counters for counting things that always go up.
	Use Gauges for things that can go up or down.
	Use Histograms or Summaries for distributions of values.
*/
package metrics
