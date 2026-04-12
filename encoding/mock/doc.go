/*
Package mockencoding provides moq-generated mocks for the encoding package.
*/
package mockencoding

// Regenerate the moq mocks via `go generate ./encoding/mock/`.

//go:generate go tool github.com/matryer/moq -out encoder_decoder_mock.go -pkg mockencoding -rm -fmt goimports .. ServerEncoderDecoder:ServerEncoderDecoderMock ClientEncoder:ClientEncoderMock
