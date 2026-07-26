// Package mock provides moq-generated mock implementations of the
// eventcapture package's interfaces.
package mock

// Regenerate the moq mocks via `go generate ./eventcapture/mock/`.

//go:generate go tool github.com/matryer/moq -out sink_mock.go -pkg mock -rm -fmt goimports .. Sink:SinkMock
