/*
Package httpclient constructs HTTP clients with optional OpenTelemetry tracing instrumentation.

Clients are built with functional options:

	client := httpclient.NewHTTPClient(
		httpclient.WithTimeout(5*time.Second),
		httpclient.WithTracing(true),
	)

Options are applied in order, so a later one overrides an earlier one. An
environment-loaded Config expresses itself as Options via Config.Options, so a
config-driven client is built the same way, and individual settings can still be
overridden after it:

	client := httpclient.NewHTTPClient(append(cfg.Options(), httpclient.WithTracing(true))...)
*/
package httpclient
