package links

import (
	"context"
	"time"
)

// Store is where a minted link's record lives.
//
// This package ships two implementations — links/cache over a
// cache.Cache[Record], and links/database over a SQL table with its own
// migrations — and which one a deployment picks is not a performance choice.
// A link is minted by whatever builds the email and redeemed by whatever
// serves the click, and those are routinely two processes. A cache backed by
// Redis is shared between them; one backed by cache/memory is not, so a link
// minted in the message handler does not exist for the API server, and every
// outstanding link dies at the next deploy besides.
//
// What an implementation owes a Minter is not "these three methods". It is the
// three properties they exist to hold, none of which the signatures can state:
//
// A record is stored under the digest of a token and never beside the token.
// Nothing this interface carries is the credential, and nothing an
// implementation writes down may be.
//
// Resolve is atomic. Two calls naming one link must produce one transition and
// one refusal, on separate connections, with no cooperation from the Minter —
// that single guarantee is the whole of single use, and it is the reason the
// method exists rather than the Minter reading a record and writing it back.
// links/cache buys it with a distributedlock; links/database buys it with the
// affected row count of a guarded UPDATE, which is why that one needs no lock
// service at all.
//
// Expiry is refused rather than reclaimed. Record.Usable decides it, against
// the instant Resolve was handed, so a store that evicts late — or not at all,
// as cache/memory does for an entry nothing reads, and as a table does until
// something sweeps it — cannot keep a credential alive past the moment it was
// supposed to die.
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
}
