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

// SubjectRevoker is an optional Store capability: withdrawing every link still
// live for one subject, in one operation.
//
// It is not on Store, and that is a decision about what the two shipped
// implementations can honestly promise rather than a tidiness preference. A
// link's record is stored under the digest of its token, because redemption
// knows the digest and nothing else — so subject cannot be folded into the key.
// links/database keeps it in a column and answers this with one statement;
// cache.Cache offers no read by a value's field at all, so links/cache could
// only answer it by maintaining a subject-to-IDs index of its own: a second
// write per mint that can fail independently of the first, and a set that
// drifts from the records it points at whenever either write loses. That is the
// "second, weaker copy of a log the application already keeps" this package
// declined once already, and building it inside the package that declined it
// would not make it a better idea.
//
// So the capability is the shape it is, and the shape has a precedent:
// postgres.PgxAccess is the same thing one layer down — something the base
// interface does not carry, reachable from the implementation that has it.
//
//	revoker, ok := store.(links.SubjectRevoker)
//
// A Minter does that assertion for its caller and reports
// ErrSubjectRevocationUnsupported when it fails; see Minter.RevokeForSubject.
// The assertion survives the config subpackage, which narrows the concrete
// store to a Store on the way out — an interface value keeps the dynamic type
// it was given — so a Minter assembled from configuration over the database
// provider has this and one over the cache provider does not.
type SubjectRevoker interface {
	// RevokeForSubject moves every unresolved link for a subject into
	// StateRevoked, and reports how many it moved.
	//
	// at is the instant the revocation happens at and purgeAfter is when the
	// store may forget the rows it moved. Both come from the Minter's clock,
	// exactly as Resolve takes them, so one clock decides every stamp this
	// package writes.
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
