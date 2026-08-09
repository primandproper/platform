/*
Package encryptionmock provides moq-generated mock implementations of the
encryption package's interfaces.
*/
package encryptionmock

// Regenerate the moq mocks via `go generate ./cryptography/encryption/mock/`.

//go:generate go tool github.com/matryer/moq -out encryptor_decryptor_mock.go -pkg encryptionmock -rm -fmt goimports .. EncryptorDecryptor:EncryptorDecryptorMock
//go:generate go tool github.com/matryer/moq -out cipher_mock.go -pkg encryptionmock -rm -fmt goimports .. Cipher:CipherMock
//go:generate go tool github.com/matryer/moq -out key_wrapper_mock.go -pkg encryptionmock -rm -fmt goimports .. KeyWrapper:KeyWrapperMock
