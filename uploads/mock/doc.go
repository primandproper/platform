// Package mockuploads provides mock implementations of the uploads package's
// interfaces. Both the hand-written testify-based MockUploadManager and the
// moq-generated UploadManagerMock live here during the testify → moq migration.
// New test code should prefer UploadManagerMock.
package mockuploads

// Regenerate the moq mocks via `go generate ./uploads/mock/`.

//go:generate go tool github.com/matryer/moq -out upload_manager_mock.go -pkg mockuploads -rm -fmt goimports .. UploadManager:UploadManagerMock
