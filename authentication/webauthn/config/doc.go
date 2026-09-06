// Package webauthncfg assembles a WebAuthn relying party, and a cache-backed
// ceremony store, from environment configuration.
//
// A relying party is the domain, the display name, the permitted origins and
// the ceremony deadline, over a store that holds a challenge for the seconds
// between the two halves of a ceremony. Nothing here owns a table.
//
// Use the redis cache. The memory provider is per-process, so a challenge issued
// by one replica cannot be answered on another, and a fleet running it fails a
// fraction of registrations and logins while looking configured. A deployment
// that would rather hold ceremony state in SQL — which is what most should do —
// calls webauthndbcfg instead; that package selects between this store and the
// table, and delegates here for the half it does not own.
//
// # Why there is no Provider field
//
// There was one, selecting between a SQL table and a cache, defaulting to the
// table. It has moved to webauthndbcfg, along with the Database block it gated
// and the SweepInterval only the table reads.
//
// The rule behind the move is the one authorizationcfg and oauth2servercfg
// record too: the provider string exists because a second implementation exists,
// so it belongs with the implementation that created the choice. Take the table
// away and there is one store left to build, which is nothing to select between.
//
// The move was forced rather than chosen. authentication/webauthn is a protocol
// engine and leaves for primitives-go; authentication/webauthn/database owns the
// ceremony table and stays. A config that dispatched on a provider string named
// both, so it could travel with neither.
//
// Note what did not move: the default. That the table is the default, and why —
// between a loud failure and a quiet one, the loud one — is a property of the
// choice, so it lives with the choice, in webauthndbcfg. A deployment reaching
// this package directly has said it wants the cache.
//
// # Why NewRelyingParty takes a store
//
// It used to take a database.Client and build the store itself, which is what
// made it name the table package. Now it takes the webauthn.SessionStore, and
// that is the line the whole split runs along: a constructor that receives a
// store crosses nothing, and a constructor that builds one by dispatching on a
// provider string names every package it might build from.
//
// Both shapes are still available. webauthndbcfg.NewRelyingParty takes the
// client and does the selecting, so a caller wiring the common case writes what
// it wrote before.
//
// # Why it was not the other two exits
//
// Keeping the config whole on the domain side would leave webauthn shipping no
// config subpackage at all, against the convention every other package here
// follows, so a consumer wanting the protocol engine would import the module
// that owns the ceremony table to configure it. That cost is paid forever, at
// every wiring site.
//
// Conceding that an interface with a table-owning implementation is a domain
// noun would keep webauthn here whole. It is coherent, and it is a bigger claim
// than this one field: it moves four packages back across a line drawn
// deliberately.
package webauthncfg
