// Package meteringmock provides moq-generated mock implementations of interfaces in the
// metering package. The primary consumers are external tests that need to stand
// in for metering.Store, metering.Recorder, or metering.Enforcer without a
// database — a handler test asserting that an endpoint refuses a request over
// quota, most often.
//
// PeriodResolver, QuotaSource, ProviderMapper, and EntitlementReader are
// deliberately absent. All four are single-method interfaces with function
// adapters in the package itself (PeriodResolverFunc, QuotaSourceFunc,
// ProviderMapperFunc, EntitlementReaderFunc), and a closure is a better test
// double than a generated struct when the whole implementation is one method.
//
// There is nothing here for the Flusher either. It is a concrete background loop
// rather than a seam, and a test that wants to drive one calls its methods
// against a real store, which is the only way to observe what it actually does.
package meteringmock

// Regenerate the moq mocks via `go generate ./metering/mock/`.

//go:generate go tool github.com/matryer/moq -out metering_mock.go -pkg meteringmock -rm -fmt goimports .. Store:StoreMock Recorder:RecorderMock Enforcer:EnforcerMock
