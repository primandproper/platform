// Package sagamock provides moq-generated mock implementations of interfaces in
// the saga package. The primary consumer is an external test that needs to
// stand in for saga.Store without a database — a handler test asserting that an
// endpoint starts the saga it should, most often.
//
// Runner is deliberately absent. It is generic over the state type, and a mock
// of a generic interface has to be generated per instantiation; a test that
// needs one is better served by a real Runner over this Store mock, which
// exercises the encoding and the type check as well.
//
// EventPublisher is absent for the opposite reason: it is a single-method
// interface with a function adapter in the package itself
// (EventPublisherFunc), and a closure is a better test double than a generated
// struct when the whole implementation is one method.
//
// There is nothing here for the Worker either. It is a concrete background loop
// rather than a seam, and a test that wants to drive one calls its methods
// against a real database, which is the only way to observe what it actually
// does.
package sagamock

// Regenerate the moq mocks via `go generate ./saga/mock/`.

//go:generate go tool github.com/matryer/moq -out saga_mock.go -pkg sagamock -rm -fmt goimports .. Store:StoreMock
