// Package cachemock provides moq-generated mock implementations of interfaces in
// the cache package. The primary consumer is external tests that need to mock
// cache.Cache[T] — cache's own tests do not depend on this package. Batched
// operations are part of Cache itself, so CacheMock covers them too.
package cachemock

// Regenerate via `go generate ./cache/mock/`.

//go:generate go tool github.com/matryer/moq -out cache_mock.go -pkg cachemock -rm -fmt goimports .. Cache:CacheMock
