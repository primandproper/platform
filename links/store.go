package links

import (
	"context"
	"time"
)

// Store is where a minted link's record lives.
//
// This module ships one implementation, links/database, over a SQL table with
// its own migrations. A cache-backed store shipped alongside it once and was
// withdrawn; links/doc.go records why, because the argument is about what this
// interface can honestly promise rather than about which storage is faster.
//
// What an implementation owes a Minter is not "these four methods". It is the
// properties they exist to hold, none of which the signatures can state:
//
// A record is stored under the digest of a token and never beside the token.
// Nothing this interface carries is the credential, and nothing an
// implementation writes down may be.
//
// Resolve is atomic. Two calls naming one link must produce one transition and
// one refusal, on separate connections, with no cooperation from the Minter —
// that single guarantee is the whole of single use, and it is the reason the
// method exists rather than the Minter reading a record and writing it back.
// links/database buys it with the affected row count of a guarded UPDATE, which
// is why it needs no lock service; an implementation that cannot state where
// its own atomicity comes from does not have any.
//
// A subject is a column, not a derived index. RevokeForSubject is on this
// interface rather than beside it, so an implementation that cannot read
// records by subject cannot satisfy it — see the method.
//
// Expiry is refused rather than reclaimed. Record.Usable decides it, against
// the instant Resolve was handed, so a store that evicts late — or not at all,
// as a table does until something sweeps it — cannot keep a credential alive
// past the moment it was supposed to die.
type Store interface {
	// Put writes a freshly minted link's record.
	//
	// It takes no lifetime argument because the record carries both ends of
	// one: CreatedAt is when it was written and PurgeAfter is when the store
	// may forget it, so an implementation that wants a duration subtracts and
	// reads no clock of its own.
	//
	// The id is already in use only when the token generator has repeated
	// itself, so an implementation refuses rather than overwrites: a failed
	// mint is the correct outcome of randomness that has stopped being random.
	Put(ctx context.Context, id ID, record *Record) error

	// Get reads a record without consuming it.
	//
	// It reports ErrLinkNotFound when nothing is stored under id, and
	// ErrStaleRecord when what is stored was written by a different shape of
	// this package. Every other error means the store could not answer, which
	// is a different fact with an opposite consequence — see ErrStoreUnavailable.
	Get(ctx context.Context, id ID) (*Record, error)

	// Resolve moves an active link into a terminal state, atomically, and
	// returns the record it acted on.
	//
	// at is the instant the transition happens at and the instant expiry is
	// decided against; purgeAfter is when the store may forget the resolved
	// record. Both come from the Minter's clock, so a test clock that only
	// moves when a test moves it decides both sides of every comparison.
	//
	// A nil error means this caller, and no other, moved the link. When the
	// link answers for itself instead — absent, already spent, revoked, or
	// expired — the error is that answer and the record is returned alongside
	// it whenever one was found, because the action it names is what keeps a
	// metric labeled by action from going blank exactly when one flow's links
	// start failing.
	Resolve(ctx context.Context, id ID, to State, at, purgeAfter time.Time) (*Record, error)

	// RevokeForSubject moves every unresolved link for a subject into
	// StateRevoked, and reports how many it moved.
	//
	// at is the instant the revocation happens at and purgeAfter is when the
	// store may forget the rows it moved. Both come from the Minter's clock,
	// exactly as Resolve takes them, so one clock decides every stamp this
	// package writes.
	//
	// It is on this interface rather than beside it as an optional capability,
	// and that placement is the decision the cache-backed store lost. A record
	// is keyed by the digest of its token, because redemption arrives holding
	// the token and nothing else, so an implementation that can only read by
	// key cannot answer this at all — it could only keep a subject-to-IDs set
	// of its own, which is a second write per mint that can fail alone and a
	// set that drifts from the records it points at. Rather than describe that
	// gap in a capability interface, a sentinel and a type assertion every
	// caller had to handle, the interface requires the column and the store
	// that could not hold one was withdrawn. An implementation storing records
	// where subject cannot be read is not a Store.
	//
	// There is no scope argument, and there will not be one. Revoking a
	// person's links should cross whatever tenants that person belongs to
	// rather than stop inside one: the caller asking is a completed password
	// reset, a locked account, or an erasure, and none of those means "in this
	// tenant only".
	//
	// It does not consult a link's deadline, because nothing in this package
	// decides liveness in SQL — Record.Usable does, in Go, against the Minter's
	// clock. A link that expired without ever being resolved is therefore moved
	// too, and the count includes it.
	RevokeForSubject(ctx context.Context, subject Subject, at, purgeAfter time.Time) (int64, error)
}
