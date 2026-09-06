package webauthncfg

import (
	"context"

	"github.com/primandproper/platform-go/v14/authentication/webauthn"
	"github.com/primandproper/platform-go/v14/observability"

	"github.com/samber/do/v2"
)

// RegisterSessionStore registers a webauthn.SessionStore with the injector,
// backed by the cache this package's Config describes.
//
// It resolves no database.Client, which is the whole reason a container running
// ceremony state in a cache can be built from this half alone. A container that
// wants the store selected from a provider string registers
// webauthndbcfg.RegisterSessionStore instead — the two provide the same key, so
// exactly one of them belongs in any given injector.
//
// Prerequisites: context.Context and *Config must be registered.
func RegisterSessionStore(i do.Injector) {
	do.Provide(i, func(i do.Injector) (webauthn.SessionStore, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewSessionStore(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			WithPillars(pillars),
		)
	})
}

// RegisterRelyingParty registers a *webauthn.RelyingParty with the injector,
// over the webauthn.SessionStore the container already holds.
//
// It resolves that store rather than building one, which is a change from when
// this package could build either kind: a relying party that built its own store
// would leave a container holding two, with only one of them in the ceremonies.
// Now there is one registration of the store and one thing reading it, so the
// hazard is gone rather than guarded against — register the store first, from
// this package or from webauthndbcfg, and this reads whichever it was.
//
// Prerequisites: context.Context, *Config and a webauthn.SessionStore must be
// registered.
func RegisterRelyingParty(i do.Injector) {
	do.Provide(i, func(i do.Injector) (*webauthn.RelyingParty, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		store, err := do.Invoke[webauthn.SessionStore](i)
		if err != nil {
			return nil, err
		}

		return NewRelyingParty(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			store,
			WithPillars(pillars),
		)
	})
}
