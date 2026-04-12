// Package mock provides mock implementations of the llm package's interfaces.
// Both the hand-written testify-based Provider and the moq-generated
// ProviderMock live here during the testify → moq migration. New test code
// should prefer ProviderMock.
package mock

// Regenerate the moq mocks via `go generate ./llm/mock/`.

//go:generate go tool github.com/matryer/moq -out provider_mock.go -pkg mock -rm -fmt goimports .. Provider:ProviderMock
