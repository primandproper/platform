// Package llmmock provides mock implementations of the llm package's interfaces.
package llmmock

// Regenerate the moq mocks via `go generate ./llm/mock/`.

//go:generate go tool github.com/matryer/moq -out provider_mock.go -pkg llmmock -rm -fmt goimports .. Provider:ProviderMock Stream:StreamMock
