package encryptioncfg

import (
	"context"

	"github.com/primandproper/platform-go/v10/cryptography/encryption"
	"github.com/primandproper/platform-go/v10/observability"

	"github.com/samber/do/v2"
)

// RegisterEncryptorDecryptor registers an encryption.EncryptorDecryptor with the injector.
//
// Consumers must provide an encryption.MasterKey into the container (e.g. via
// do.ProvideValue(i, encryption.MasterKey(keyBytes))). The master key is resolved
// as the named encryption.MasterKey type rather than a bare []byte so it cannot
// collide with an unrelated []byte value registered in the same container.
func RegisterEncryptorDecryptor(i do.Injector) {
	do.Provide[encryption.EncryptorDecryptor](i, func(i do.Injector) (encryption.EncryptorDecryptor, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewEncryptorDecryptor(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			do.MustInvoke[encryption.MasterKey](i),
			WithPillars(pillars),
		)
	})
}
