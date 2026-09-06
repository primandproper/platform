// Package oauth2servercfg assembles an OAuth 2.1 authorization server, and an
// in-memory Store, from environment configuration.
//
// The memory store is for tests and single-process development. The limit is not
// a performance one: an authorization code issued by one replica cannot be
// redeemed at another, so a fleet running it fails logins in proportion to how
// well its load balancer works. A deployment keeping records in SQL — which is
// what a deployment wants — calls oauth2dbcfg instead; that package selects
// between the table and this store, and delegates here for the half it does not
// own.
//
// NewStore builds the store alone. NewServer builds the server over a store it
// is handed, and needs the things this package cannot configure: a
// SubjectAuthenticator that knows who the human is, and — optionally — a
// LoginRenderer that draws the form and a SubjectResolver that recognizes a
// resource owner who is already signed in. The optional two arrive through
// WithServerOptions.
//
// # Why there is no Provider field
//
// There was one, selecting between memory and a SQL table and defaulting to the
// table. It has moved to oauth2dbcfg, along with the Database block it gated.
//
// The rule behind the move is the one authorizationcfg and webauthncfg record
// too: the provider string exists because a second implementation exists, so it
// belongs with the implementation that created the choice. Take the tables away
// and there is one store left to build, which is nothing to select between.
//
// The move was forced rather than chosen. authentication/oauth2server is a
// protocol implementation — grants, codes, tokens, discovery — and leaves for
// primitives-go; authentication/oauth2server/database owns the client and token
// tables and stays. A config that dispatched on a provider string named both, so
// it could travel with neither.
//
// What did not move is SweepInterval, because both stores sweep. Its home is the
// question of which package reads it, not which one is the default.
//
// # Why NewServer takes a store
//
// It used to take a database.Client and build the store itself, which is what
// made it name the tables package. Now it takes the oauth2server.Store, and that
// is the line the whole split runs along: a constructor that receives a store
// crosses nothing, and a constructor that builds one by dispatching on a
// provider string names every package it might build from.
//
// Both shapes are still available. oauth2dbcfg.NewServer takes the client and
// does the selecting, so a caller wiring the common case writes what it wrote
// before.
//
// # Why it was not the other two exits
//
// Keeping the config whole on the domain side would leave oauth2server shipping
// no config subpackage at all, against the convention every other package here
// follows, so a consumer wanting the protocol implementation would import the
// module that owns the token tables to configure it. That cost is paid forever,
// at every wiring site.
//
// Conceding that an interface with a table-owning implementation is a domain
// noun would keep oauth2server here whole. It is coherent, and it is a bigger
// claim than this one field: it moves four packages back across a line drawn
// deliberately.
package oauth2servercfg
