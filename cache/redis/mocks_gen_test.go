package redis

// Regenerate the redisClient mock via `go generate ./cache/redis/...`. The
// redisClient interface is unexported (it's a test seam), so its mock lives
// in-package as a *_test.go file rather than under a sibling mock package.
// Mocks for the external interfaces (metrics.Provider, circuitbreaking.CircuitBreaker)
// live alongside those interfaces in observability/metrics/mock and
// circuitbreaking/mock.

//go:generate go tool github.com/matryer/moq -out redisclient_mock_test.go -pkg redis -rm -fmt goimports . redisClient
