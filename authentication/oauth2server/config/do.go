package oauth2servercfg

import (
	"context"

	"github.com/primandproper/platform-go/v14/authentication/oauth2server"
	"github.com/primandproper/platform-go/v14/observability"

	"github.com/samber/do/v2"
)

// RegisterStore registers an oauth2server.Store with the injector, backed by
// memory.
//
// It resolves no database.Client, which is the whole reason a container running
// a single-process server can be built from this half alone. A container that
// wants the store selected from a provider string registers
// oauth2dbcfg.RegisterStore instead — the two provide the same key, so exactly
// one of them belongs in any given injector.
//
// Prerequisites: context.Context and *Config must be registered.
func RegisterStore(i do.Injector) {
	do.Provide(i, func(i do.Injector) (oauth2server.Store, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewStore(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			WithPillars(pillars),
		)
	})
}

// RegisterServer registers a *oauth2server.Server with the injector, over the
// oauth2server.Store the container already holds.
//
// It resolves that store rather than building one, so whichever half registered
// it is the one the server issues codes against. Register a store first, from
// this package or from oauth2dbcfg.
//
// The oauth2server.SubjectAuthenticator is a hard prerequisite rather than an
// optional resolution: it is how this deployment identifies a human, and a
// container that has not registered one should fail to come up rather than
// build a server that issues authorization codes to whoever asks.
//
// Prerequisites: context.Context, *Config, an oauth2server.Store and an
// oauth2server.SubjectAuthenticator must be registered.
func RegisterServer(i do.Injector) {
	do.Provide(i, func(i do.Injector) (*oauth2server.Server, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		store, err := do.Invoke[oauth2server.Store](i)
		if err != nil {
			return nil, err
		}

		return NewServer(
			do.MustInvoke[context.Context](i),
			do.MustInvoke[*Config](i),
			store,
			do.MustInvoke[oauth2server.SubjectAuthenticator](i),
			WithPillars(pillars),
		)
	})
}
