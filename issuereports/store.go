package issuereports

import (
	"context"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/tenancy"
)

// Store is the persistence seam for issue reports.
//
// This package ships a SQL implementation ([NewSQLStore]) together with the DDL
// it needs (issuereports/migrations), so adopting it does not mean writing this.
// The interface exists because a report queue and its storage are genuinely
// separable, and an application with its own schema conventions should not have
// to fork the package to keep them.
//
// Every method takes a tenancy.Scope, and none of them offers a variant that
// omits it — an implementation must filter on it rather than treat it as a hint.
// A deployment with one tenant passes tenancy.Global() everywhere and behaves
// exactly as it would have without the column.
//
// There is deliberately no cross-scope listing, and it is worth being clear
// about what that costs. An operator triaging every tenant's reports from one
// console is a real thing to want, and this interface will not answer it in one
// call: they list the scopes they administer and page each. The alternative is a
// read that omits the scope, which is the one read that cannot tell an
// operator's caller from a tenant's — and a paged list cannot bind a set of
// scopes either, because a bound set may not sit in a statement that also binds
// a cursor and a page size on two of the three dialects this package serves.
type Store interface {
	// CreateReport files one report, under the scope the value carries. It
	// assigns the id where the caller left it empty, sets the status to
	// StatusOpen, and writes back what was stored.
	//
	// A report is born open, so a value arriving in any other status is
	// ErrInvalidStatusTransition rather than a stored row: a report that started
	// resolved is one nobody resolved.
	CreateReport(ctx context.Context, report *Report) error

	// CreateReportTx is CreateReport inside the caller's transaction, so the
	// report commits with whatever the caller writes beside it. A nil q is an
	// error wrapping ErrNilExecutor.
	//
	// It exists for the reason DeleteReportsByReporter takes an executor, at the
	// other end of a report's life. A row in a consumer's schema is rarely
	// written alone: an audit entry naming who filed it and a data change event
	// on an outbox somebody fans out are the ordinary companions, and a
	// companion is worth what its atomicity with the row is worth. Written after
	// this method's own write has committed, they are a window in which the
	// report exists and nothing downstream has been told — narrow,
	// one-directional, and still not something a consumer can close from
	// outside this package.
	//
	// Every check CreateReport makes is made here, and the read-back of the
	// creation time runs on q, so the value handed back is the row this
	// transaction wrote rather than one waiting on a commit.
	CreateReportTx(ctx context.Context, q database.Tx, report *Report) error

	// GetReport reads one of the scope's live reports. It returns an error
	// wrapping ErrReportNotFound when the report does not exist, has been
	// archived, or belongs to another scope — which are the same answer from
	// here.
	GetReport(ctx context.Context, scope tenancy.Scope, reportID string) (*Report, error)

	// ListReports pages the scope's reports, in the direction the filter names.
	ListReports(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Report], error)

	// ListReportsByStatus is ListReports restricted to one status: the triage
	// queue.
	//
	// It is a separate method rather than a field on the filter because the
	// filter is this module's shared page description and a status is this
	// package's own. The count a console wants beside the queue is on the
	// result's pagination: the filtered count is of everything in that status,
	// not of the page.
	//
	// A status this package does not serve is ErrUnknownStatus rather than an
	// empty page, because an empty page is what a queue that has been quietly
	// misspelled looks like.
	ListReportsByStatus(ctx context.Context, scope tenancy.Scope, status Status, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Report], error)

	// ListReportsByReporter pages the reports one person filed within the scope.
	// It is what a "your reports" view reads, and what the subject access
	// request collector pages through.
	ListReportsByReporter(ctx context.Context, scope tenancy.Scope, reporter string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Report], error)

	// ListReportsBySubjectType pages every report about one kind of thing —
	// "everything anybody has said about recipes".
	ListReportsBySubjectType(ctx context.Context, scope tenancy.Scope, subjectType string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Report], error)

	// ListReportsForSubject pages every report about one particular thing.
	ListReportsForSubject(ctx context.Context, scope tenancy.Scope, subjectType, subjectID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Report], error)

	// UpdateReport revises what the reporter said: the kind, the details, and
	// what the report is about.
	//
	// It does not move the status, and it cannot: the lifecycle has one door and
	// it is TransitionReport, which names the status it believed the row was in.
	// A whole-row write that also assigned the status would be a revision that
	// silently reopened a report somebody had just resolved.
	//
	// A report that is not in the scope — absent, archived, or somebody else's —
	// is an error wrapping ErrReportNotFound.
	UpdateReport(ctx context.Context, report *Report) error

	// UpdateReportTx is UpdateReport inside the caller's transaction, so the
	// revision commits with whatever the caller records about it. A nil q is an
	// error wrapping ErrNilExecutor.
	//
	// A revision is an event as much as it is a write — who changed what, and
	// when — and the entry saying so belongs in the transaction that made the
	// change rather than in one after it. See CreateReportTx for the argument
	// in full.
	UpdateReportTx(ctx context.Context, q database.Tx, report *Report) error

	// TransitionReport moves a report from one status to another and returns it
	// as stored.
	//
	// from is the status the caller believes the report is in, and it is a
	// parameter rather than something this method reads for itself because that
	// is what makes the move safe under concurrency: the statement requires the
	// row to still hold it. Two triagers resolving the same report means one of
	// them gets ErrStatusConflict rather than both being told they won and the
	// second note overwriting the first.
	//
	// resolution is why — the note a triager leaves. A move into a terminal
	// status stamps ClosedAt and stores the note; a move out of one clears both,
	// because a reason that no longer holds is worse than none.
	//
	// A move the lifecycle does not admit is ErrInvalidStatusTransition and
	// nothing is written. See [Status.CanTransitionTo].
	TransitionReport(ctx context.Context, scope tenancy.Scope, reportID string, from, to Status, resolution string) (*Report, error)

	// TransitionReportTx is TransitionReport inside the caller's transaction,
	// so the move commits with the entry naming who made it and why. A nil q is
	// an error wrapping ErrNilExecutor. See CreateReportTx for the argument in
	// full.
	//
	// It differs from its twin in one more way than where it commits, and the
	// difference is worth naming. The guard and the two reads around it — the
	// one that separates a moved report from an absent one when the guard
	// matches nothing, and the read-back of the row on a hit — all run on q,
	// so they see what the caller has written and not yet committed. A report
	// filed earlier in the same transaction can be moved by this call, where
	// TransitionReport would report it absent until the commit; and the report
	// handed back is the row as this transaction has it, which is what a caller
	// recording the outcome beside it wants to be describing. What does not
	// change is the guard's meaning: a report somebody else moved between the
	// caller's read and this write is ErrStatusConflict on both paths, and
	// nothing is written.
	TransitionReportTx(ctx context.Context, q database.Tx, scope tenancy.Scope, reportID string, from, to Status, resolution string) (*Report, error)

	// ArchiveReport removes a report from the queue, leaving the row for
	// whoever asks later what was reported.
	//
	// It is not "closed": closing is a status, and an archived report is one
	// that should stop being listed at all. A report already archived is an
	// error wrapping ErrReportNotFound, because an archived report is not in the
	// queue and this method addresses the queue.
	ArchiveReport(ctx context.Context, scope tenancy.Scope, reportID string) error

	// ArchiveReportTx is ArchiveReport inside the caller's transaction, so the
	// removal commits with whatever the caller records about it. A nil q is an
	// error wrapping ErrNilExecutor.
	//
	// It is the variant a moderation action reaches for: the report leaves the
	// queue and the entry naming who removed it land together, or neither does.
	// See CreateReportTx for the argument in full.
	ArchiveReportTx(ctx context.Context, q database.Tx, scope tenancy.Scope, reportID string) error

	// DeleteReportsByReporter destroys every report one person filed within the
	// scope, archived ones included, and reports how many that was.
	//
	// It is a hard delete and it is the erasure path: the details are free text
	// somebody wrote, so what a right-to-be-forgotten request has to remove is
	// the text rather than a flag beside it. issuereports/privacy is the
	// dataprivacy.Eraser built on this; nothing else in this package deletes a
	// row.
	//
	// It runs inside the caller's transaction and must use the executor it is
	// given, so that a subject's reports and the rest of their footprint commit
	// or roll back together.
	DeleteReportsByReporter(ctx context.Context, q database.Tx, scope tenancy.Scope, reporter string) (int64, error)
}
