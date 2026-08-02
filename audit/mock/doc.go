// Package auditmock provides moq-generated mock implementations of interfaces in
// the audit package. The primary consumers are external tests that need to
// stand in for audit.Recorder or audit.Reader without a database — a handler
// test asserting that a mutation records what it should, most often.
//
// Note that there is nothing here for the Sweeper. It is a concrete background
// loop rather than a seam, and a test that wants to drive one calls Sweep
// directly against a real database, which is the only way to observe what it
// actually does.
package auditmock

// Regenerate the moq mocks via `go generate ./audit/mock/`.

//go:generate go tool github.com/matryer/moq -out audit_mock.go -pkg auditmock -rm -fmt goimports .. Recorder:RecorderMock Reader:ReaderMock
