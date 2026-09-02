package comments

import (
	"context"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/tenancy"
)

// Store is the persistence seam for comments.
//
// This package ships a SQL implementation ([NewSQLStore]) together with the DDL
// it needs (comments/migrations), so adopting it does not mean writing this. The
// interface exists because a discussion and its storage are genuinely separable,
// and an application with its own schema conventions should not have to fork the
// package to keep them.
//
// Every method takes a tenancy.Scope, and none of them offers a variant that
// omits it — an implementation must filter on it rather than treat it as a hint.
// A deployment with one tenant passes tenancy.Global() everywhere and behaves
// exactly as it would have without the column.
//
// There is deliberately no cross-scope listing, and it is worth being clear
// about what that costs. An operator moderating every tenant's comments from one
// console is a real thing to want, and this interface will not answer it in one
// call: they list the scopes they administer and page each. The alternative is a
// read that omits the scope, which is the one read that cannot tell an
// operator's caller from a tenant's — and a paged list cannot bind a set of
// scopes either, because a bound set may not sit in a statement that also binds
// a cursor and a page size on two of the three dialects this package serves.
//
// # The catalog gates writes, not reads
//
// A comment names a target type, and the catalog the store was built with is
// what says which types exist. [Store.CreateComment] refuses one the catalog
// does not hold; no read does.
//
// That is not an oversight. The catalog exists to stop a comment being written
// where nothing will ever list it — the misspelling that produces rows no view
// shows — and that failure is at the write. A read of a type the catalog does
// not hold answers with the rows that are there, which is nothing at all, since
// the write gate is what stopped any from being written. The exception is the
// type that was withdrawn, and there the rows are exactly what the operator who
// withdrew it needs to reach: gating the read would make the catalog a mechanism
// for hiding rows, which is not what it is for.
type Store interface {
	// CreateComment writes one comment, under the scope the value carries. It
	// assigns the id where the caller left it empty and writes back what was
	// stored.
	//
	// The target is checked against the catalog the store was built with, and
	// against that definition's existence hook where one is registered: an
	// unregistered type is an error wrapping ErrUnknownTargetType, and a target
	// the hook cannot find is ErrTargetNotFound.
	//
	// A comment with a ParentID is a reply, and three things are true of one. Its
	// parent must be a live comment in the same scope, or the write is
	// ErrParentNotFound. Its parent must itself be a root, or the write is
	// ErrNestedReply. And it belongs to its parent's discussion: a reply that
	// names no target adopts the parent's, and one that names a different target
	// is ErrTargetMismatch.
	CreateComment(ctx context.Context, comment *Comment) error

	// CreateCommentTx is CreateComment inside the caller's transaction, so the
	// comment commits with whatever the caller writes beside it. A nil q is an
	// error wrapping ErrNilExecutor.
	//
	// It exists for the same reason the two deletes take an executor, at the other
	// end of a comment's life. A row in a consumer's schema is rarely written
	// alone: an audit entry naming who did what and a data change event on an
	// outbox somebody fans out are the ordinary companions, and a companion is
	// worth what its atomicity with the row is worth. Written after this method's
	// own write has committed, they are a window in which the comment exists and
	// nothing downstream has been told — narrow, one-directional, and still not
	// something a consumer can close from outside this package.
	//
	// Every check CreateComment makes is made here, and the reads behind them run
	// on q. A reply whose parent was written earlier in the same transaction
	// resolves its parent, where the non-transactional path would have to wait for
	// a commit to see it.
	//
	// One check does not move into the transaction, and it is worth stating rather
	// than leaving to be found. A TargetExistsFunc is handed a scope and a target
	// id and no executor — the row it answers for lives in a table this package
	// has never seen, sometimes in another database — so it reads on whatever
	// connection the consumer built it over, which is not this one. A comment
	// filed in the same transaction that creates the thing it is about is
	// therefore ErrTargetNotFound where that target type registers a hook, and is
	// written where it does not. The alternative is an executor on the hook's
	// signature, which is a connection to a table most hooks do not read.
	CreateCommentTx(ctx context.Context, q database.Tx, comment *Comment) error

	// GetComment reads one of the scope's live comments. It returns an error
	// wrapping ErrCommentNotFound when the comment does not exist, has been
	// archived, or belongs to another scope — which are the same answer from
	// here.
	GetComment(ctx context.Context, scope tenancy.Scope, commentID string) (*Comment, error)

	// ListRootComments pages the top level of one target's discussion: the
	// comments that reply to nothing.
	//
	// It is the read a discussion opens with, and it is a separate method from
	// ListReplies rather than a parent argument a caller leaves empty, because
	// "the roots" and "this comment's replies" are two questions somebody asks
	// deliberately. Underneath they are one statement with a different bound
	// value — see comments/internal/queries — which is what keeps them from
	// answering differently.
	//
	// The count a client wants beside the discussion is on the result's
	// pagination: the filtered count is of the target's roots, not of the page.
	ListRootComments(ctx context.Context, scope tenancy.Scope, target Target, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Comment], error)

	// ListReplies pages one root comment's replies.
	//
	// The target is a parameter as well as the parent because a reply carries
	// both and the statement keys on both: a reply's target is its parent's, so
	// naming it costs the caller nothing and buys the read the index it was
	// written for.
	//
	// An empty parent is ErrEmptyParent rather than the roots, which is what the
	// empty parent means in the column — returning them would be the wrong half
	// of the discussion, with nothing about the rows saying so.
	//
	// A parent that is no longer there is not an error. A reply outlives the
	// comment it replies to — archived, or erased with its author — and it is
	// still a reply; see the package documentation.
	ListReplies(ctx context.Context, scope tenancy.Scope, target Target, parentID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Comment], error)

	// ListCommentsByTargetType pages every comment about one kind of thing —
	// "everything anybody has said about recipes", roots and replies alike.
	//
	// It is the moderation read, and it is the read an operator withdrawing a
	// target type runs first, to see what withdrawing it would strand. It does
	// not gate on the catalog, and that is why.
	ListCommentsByTargetType(ctx context.Context, scope tenancy.Scope, targetType TargetType, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Comment], error)

	// ListCommentsByAuthor pages what one person wrote within the scope. It is
	// what a "your comments" view reads, and what the subject access request
	// collector pages through.
	ListCommentsByAuthor(ctx context.Context, scope tenancy.Scope, author string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Comment], error)

	// UpdateComment revises what the author said, and only that.
	//
	// It does not move the comment: the target is what the comment is about and
	// was checked against the catalog when it was written, the parent is which
	// conversation it is in, and the author is who said it. A whole-row write
	// that assigned any of the three would be an edit that silently moved
	// somebody else's words.
	//
	// A comment that is not in the scope — absent, archived, or somebody else's —
	// is an error wrapping ErrCommentNotFound.
	UpdateComment(ctx context.Context, comment *Comment) error

	// UpdateCommentTx is UpdateComment inside the caller's transaction, so the
	// revision commits with whatever the caller records about it. A nil q is an
	// error wrapping ErrNilExecutor.
	//
	// An edit is a moderation event as much as it is a write — who changed what,
	// and when — and the entry saying so belongs in the transaction that made the
	// change rather than in one after it. See CreateCommentTx for the argument in
	// full.
	UpdateCommentTx(ctx context.Context, q database.Tx, comment *Comment) error

	// ArchiveComment removes one comment from the discussion, leaving the row for
	// whoever asks later what was said.
	//
	// It archives exactly the comment named. A root's replies stay where they
	// are, which is deliberate: a moderator removing an off-topic root has not
	// removed the answers to it, and a reply whose parent is gone is what every
	// discussion UI already renders as a reply to a removed comment. A consumer
	// that wants the whole subtree gone archives the replies too, which
	// ListReplies enumerates.
	//
	// A comment already archived is an error wrapping ErrCommentNotFound,
	// because an archived comment is not in the discussion and this method
	// addresses the discussion.
	ArchiveComment(ctx context.Context, scope tenancy.Scope, commentID string) error

	// ArchiveCommentTx is ArchiveComment inside the caller's transaction, so the
	// removal commits with whatever the caller records about it. A nil q is an
	// error wrapping ErrNilExecutor.
	//
	// It is the variant a moderation action reaches for: the comment leaves the
	// discussion and the entry naming who removed it land together, or neither
	// does. See CreateCommentTx for the argument in full.
	ArchiveCommentTx(ctx context.Context, q database.Tx, scope tenancy.Scope, commentID string) error

	// DeleteCommentsForTarget destroys every comment about one thing — replies
	// and archived rows included — and reports how many that was.
	//
	// It is the sweep the package documentation's dangling-target ruling names.
	// A comment's target lives in a table this package has never seen, so nothing
	// here cascades from that table's delete; the consumer calls this from the
	// transaction that removes the target, which is why it takes an executor
	// rather than reaching for the store's own.
	//
	// Zero is not an error: a thing nobody commented on is a thing with nothing
	// here to sweep.
	DeleteCommentsForTarget(ctx context.Context, q database.Tx, scope tenancy.Scope, target Target) (int64, error)

	// DeleteCommentsByAuthor destroys everything one person wrote within the
	// scope, archived comments included, and reports how many that was.
	//
	// It is a hard delete and it is the erasure path: the body is free text
	// somebody wrote, so what a right-to-be-forgotten request has to remove is
	// the words rather than a flag beside them. comments/privacy is the
	// dataprivacy.Eraser built on this.
	//
	// It runs inside the caller's transaction and must use the executor it is
	// given, so that a subject's comments and the rest of their footprint commit
	// or roll back together.
	DeleteCommentsByAuthor(ctx context.Context, q database.Tx, scope tenancy.Scope, author string) (int64, error)
}
