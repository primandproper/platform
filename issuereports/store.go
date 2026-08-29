package issuereports

import (
	"context"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/tenancy"
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

	// ArchiveReport removes a report from the queue, leaving the row for
	// whoever asks later what was reported.
	//
	// It is not "closed": closing is a status, and an archived report is one
	// that should stop being listed at all. A report already archived is an
	// error wrapping ErrReportNotFound, because an archived report is not in the
	// queue and this method addresses the queue.
	ArchiveReport(ctx context.Context, scope tenancy.Scope, reportID string) error

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
