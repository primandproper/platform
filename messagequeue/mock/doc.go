/*
Package mockpublishers provides moq-generated mocks for the messagequeue
package's Publisher, PublisherProvider, Consumer, and ConsumerProvider
interfaces.
*/
package mockpublishers

// Regenerate the moq mocks via `go generate ./messagequeue/mock/`.

//go:generate go tool github.com/matryer/moq -out messagequeue_mock.go -pkg mockpublishers -rm -fmt goimports .. Publisher:PublisherMock PublisherProvider:PublisherProviderMock Consumer:ConsumerMock ConsumerProvider:ConsumerProviderMock
