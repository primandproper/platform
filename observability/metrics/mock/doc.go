/*
Package metricsmock provides moq-generated mocks for the metrics package.
*/
package metricsmock

// Regenerate the moq mocks via `go generate ./observability/metrics/mock/`.

//go:generate go tool github.com/matryer/moq -out provider_mock.go -pkg metricsmock -rm -fmt goimports .. Provider:ProviderMock Int64Counter:Int64CounterMock
