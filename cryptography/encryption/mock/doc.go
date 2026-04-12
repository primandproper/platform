/*
Package encryptionmock provides mock implementations of the encryption package's
interfaces. Both the hand-written testify-based MockImpl and the moq-generated
EncryptorDecryptorMock live here during the testify → moq migration. New test
code should prefer EncryptorDecryptorMock.
*/
package encryptionmock

// Regenerate the moq mocks via `go generate ./cryptography/encryption/mock/`.

//go:generate go tool github.com/matryer/moq -out encryptor_decryptor_mock.go -pkg encryptionmock -rm -fmt goimports .. EncryptorDecryptor:EncryptorDecryptorMock
