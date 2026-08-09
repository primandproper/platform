package noop

import (
	"github.com/primandproper/platform-go/v10/secrets"

	"github.com/samber/do/v2"
)

// RegisterSecretSource registers this implementation under two keys: its own
// type, *SecretSource, and secrets.SecretSource. Both resolve to the same
// source.
//
// Naming the noop is the whole point of registering it. A container that simply
// leaves the source out gets a do.Invoke failure, which is the right answer for
// a dependency somebody forgot; a container that registers this one has said
// that every secret coming back empty is the intended behavior rather than a
// misconfiguration.
//
// Nothing is invoked and nothing is configurable: a source that holds no
// secrets has no knobs and no observability to attach.
func RegisterSecretSource(i do.Injector) {
	do.Provide(i, func(do.Injector) (*SecretSource, error) {
		return NewSecretSource(), nil
	})

	// Cannot fail: *SecretSource implements secrets.SecretSource — the compiler
	// says so at the top of noop.go — and the service it aliases was just
	// provided.
	do.MustAs[*SecretSource, secrets.SecretSource](i)
}
