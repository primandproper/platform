package encoding

// ioWriter is a moq-friendly mirror of io.Writer. moq cannot generate mocks
// from stdlib interfaces directly, so we define a structurally-identical
// interface here purely so tests can mock Write calls. Any io.Writer satisfies
// this interface (and vice versa) via Go's structural typing.
type ioWriter interface {
	Write(p []byte) (int, error)
}

//go:generate go tool github.com/matryer/moq -out io_writer_mock_test.go -pkg encoding -rm -fmt goimports . ioWriter
