/*
Package encodingmock provides moq-generated mocks for the encoding package.
*/
package encodingmock

// Regenerate the moq mocks via `go generate ./encoding/mock/`.

//go:generate go tool github.com/matryer/moq -out encoder_decoder_mock.go -pkg encodingmock -rm -fmt goimports .. ServerEncoderDecoder:ServerEncoderDecoderMock ClientEncoder:ClientEncoderMock
