// Package featureflagsmock provides mock implementations of the featureflags package's
// interfaces. Both the hand-written testify-based FeatureFlagManager and the
// moq-generated FeatureFlagManagerMock live here during the testify → moq
// migration. New test code should prefer FeatureFlagManagerMock.
package featureflagsmock

// Regenerate the moq mocks via `go generate ./featureflags/mock/`.

//go:generate go tool github.com/matryer/moq -out feature_flag_manager_mock.go -pkg featureflagsmock -rm -fmt goimports .. FeatureFlagManager:FeatureFlagManagerMock
