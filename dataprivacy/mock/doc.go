// Package mock provides moq-generated mock implementations of interfaces in
// the dataprivacy package. The primary consumers are external tests that need
// to stand in for dataprivacy.Store or dataprivacy.Service without a database —
// a handler test asserting that an endpoint submits the request it should, most
// often.
//
// Collector, Eraser, and Notifier are deliberately absent. All three are
// single-method interfaces with function adapters in the package itself
// (CollectorFunc, EraserFunc, NotifierFunc), and a closure is a better test
// double than a generated struct when the whole implementation is one method.
//
// There is nothing here for the Worker or the Sweeper either. Both are concrete
// background loops rather than seams, and a test that wants to drive one calls
// its methods against a real database, which is the only way to observe what it
// actually does.
package mock

// Regenerate the moq mocks via `go generate ./dataprivacy/mock/`.

//go:generate go tool github.com/matryer/moq -out dataprivacy_mock.go -pkg mock -rm -fmt goimports .. Store:StoreMock Service:ServiceMock
