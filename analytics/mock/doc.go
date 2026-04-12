/*
Package analyticsmock provides moq-generated mocks for the analytics package.
*/
package analyticsmock

// Regenerate the moq mocks via `go generate ./analytics/mock/`.

//go:generate go tool github.com/matryer/moq -out event_reporter_mock.go -pkg analyticsmock -rm -fmt goimports .. EventReporter:EventReporterMock
