// Package mockrouting provides mock implementations of the routing package's
// interfaces. Both the hand-written testify-based RouteParamManager and the
// moq-generated RouteParamManagerMock live here during the testify → moq
// migration. New test code should prefer RouteParamManagerMock.
package mockrouting

// Regenerate the moq mocks via `go generate ./routing/mock/`.

//go:generate go tool github.com/matryer/moq -out route_param_manager_mock.go -pkg mockrouting -rm -fmt goimports .. RouteParamManager:RouteParamManagerMock
