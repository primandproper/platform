package grpc_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/identity"
	identitygrpc "github.com/primandproper/platform-go/v14/identity/grpc"
	"github.com/primandproper/platform-go/v14/identity/identitypb"
	"github.com/primandproper/platform-go/v14/observability/metrics"
	metricsmock "github.com/primandproper/platform-go/v14/observability/metrics/mock"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNewServerRefusesItsMissingDependencies(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	for name, build := range map[string]func() (*identitygrpc.Server, error){
		"nil client": func() (*identitygrpc.Server, error) {
			return identitygrpc.NewServer(nil, h.svc, h.store, extractPrincipal)
		},
		"nil service": func() (*identitygrpc.Server, error) {
			return identitygrpc.NewServer(h.db, nil, h.store, extractPrincipal)
		},
		"nil store": func() (*identitygrpc.Server, error) {
			return identitygrpc.NewServer(h.db, h.svc, nil, extractPrincipal)
		},
		// The one that would otherwise degrade rather than fail: a server with
		// no way to resolve a caller would answer every read with the zero
		// scope, which is a real directory rather than an empty one.
		"nil principal extractor": func() (*identitygrpc.Server, error) {
			return identitygrpc.NewServer(h.db, h.svc, h.store, nil)
		},
	} {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			srv, err := build()
			test.Nil(t, srv)
			test.Error(t, err)
		})
	}
}

// TestEveryRPCRefusesAnAnonymousCaller is one test rather than twenty-eight
// because it is one property, and the property is the reason the extractor is
// not optional: there is no anonymous read here, since a read with no principal
// has no scope to filter on.
func TestEveryRPCRefusesAnAnonymousCaller(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	// No principal on this context, which is what an unauthenticated request
	// looks like once the consumer's interceptor has declined to add one.
	ctx := T.Context()

	calls := map[string]func() error{
		"Register": func() error {
			_, err := h.client.Register(ctx, &identitypb.RegisterRequest{})

			return err
		},
		"GetPrincipal": func() error {
			_, err := h.client.GetPrincipal(ctx, &identitypb.GetPrincipalRequest{})

			return err
		},
		"GetUser": func() error {
			_, err := h.client.GetUser(ctx, &identitypb.GetUserRequest{UserId: "x"})

			return err
		},
		"ListUsers": func() error {
			_, err := h.client.ListUsers(ctx, &identitypb.ListUsersRequest{})

			return err
		},
		"UpdateProfile": func() error {
			_, err := h.client.UpdateProfile(ctx, &identitypb.UpdateProfileRequest{})

			return err
		},
		"ArchiveUser": func() error {
			_, err := h.client.ArchiveUser(ctx, &identitypb.ArchiveUserRequest{UserId: "x"})

			return err
		},
		"Invite": func() error {
			_, err := h.client.Invite(ctx, &identitypb.InviteRequest{AccountId: "x"})

			return err
		},
	}

	for name, call := range calls {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			err := call()
			must.Error(t, err)
			test.EqOp(t, codes.Unauthenticated, status.Code(err))
		})
	}
}

func TestRegisterWritesTheUserTheAccountAndTheMembership(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	response, err := h.client.Register(h.ctx(), &identitypb.RegisterRequest{
		User: &identitypb.UserRegistrationInput{
			Username:     "somebody",
			EmailAddress: "somebody@example.com",
			FirstName:    "Some",
			LastName:     "Body",
		},
		Account:    &identitypb.AccountCreationInput{Name: "Acme", TimeZone: "UTC"},
		OwnerRoles: []string{"owner"},
	})
	must.NoError(T, err)

	registration := response.GetRegistration()
	must.NotNil(T, registration)

	test.NotEqOp(T, "", registration.GetUser().GetId())
	test.EqOp(T, "somebody", registration.GetUser().GetUsername())

	// The registrant owns the account, and the membership that makes them a
	// member of it exists — which is the whole reason Register is one operation
	// rather than three calls.
	test.EqOp(T, registration.GetUser().GetId(), registration.GetAccount().GetOwnerUserId())
	test.EqOp(T, registration.GetAccount().GetId(), registration.GetMembership().GetBelongsToAccount())
	test.True(T, registration.GetMembership().GetDefaultAccount(),
		test.Sprint("a registrant's only account should be where they land"))
}

// TestRegisterSurfacesACollisionAsAlreadyExists is the test the error mappers
// exist for. Without them this arrives as codes.Unknown, and a client cannot
// tell "pick another username" from "try again later".
func TestRegisterSurfacesACollisionAsAlreadyExists(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	request := &identitypb.RegisterRequest{
		User:       &identitypb.UserRegistrationInput{Username: "taken", EmailAddress: "taken@example.com"},
		Account:    &identitypb.AccountCreationInput{Name: "Acme"},
		OwnerRoles: []string{"owner"},
	}

	_, err := h.client.Register(h.ctx(), request)
	must.NoError(T, err)

	_, err = h.client.Register(h.ctx(), &identitypb.RegisterRequest{
		User:       &identitypb.UserRegistrationInput{Username: "taken", EmailAddress: "other@example.com"},
		Account:    &identitypb.AccountCreationInput{Name: "Acme Two"},
		OwnerRoles: []string{"owner"},
	})
	must.Error(T, err)

	test.EqOp(T, codes.AlreadyExists, status.Code(err))

	// And the sentinel itself survives the wire, which is what the encoding and
	// decoding interceptors are for: a caller can branch on the error rather
	// than on the code.
	//
	// The standard library's errors.Is, deliberately. What crosses a connection
	// is the error's cockroachdb mark rather than the sentinel's identity, so
	// this only works because the decoding interceptor's error implements Is —
	// and that it works is the property worth pinning, since every caller will
	// reach for this matcher and not another one.
	test.True(T, errors.Is(err, identity.ErrUsernameTaken), test.Sprintf(
		"the username collision did not survive the wire as its sentinel: %v", err))

	// And the status message is the sentinel's own words, not the handler's
	// description and not the code's name. This is what a client with no access
	// to the encoded details reads, and AlreadyExists alone cannot tell a
	// username collision from an email one.
	st, ok := status.FromError(err)
	must.True(T, ok)
	test.EqOp(T, identity.ErrUsernameTaken.Error(), st.Message())
}

func TestGetUserSurfacesAnAbsenceAsNotFound(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	_, err := h.client.GetUser(h.ctx(), &identitypb.GetUserRequest{UserId: "nobody"})
	must.Error(T, err)

	test.EqOp(T, codes.NotFound, status.Code(err))
	test.True(T, errors.Is(err, identity.ErrUserNotFound))
}

// TestAReadIsScopedToTheCallersDirectory is the property the whole principal
// seam exists for: the scope comes off the caller, so a caller in one directory
// cannot see another's rows and has no field to ask with.
func TestAReadIsScopedToTheCallersDirectory(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	mine := h.seedUser(T, testScope, "mine")
	theirs := h.seedUser(T, otherScope, "theirs")

	found, err := h.client.GetUser(h.ctx(), &identitypb.GetUserRequest{UserId: mine.ID})
	must.NoError(T, err)
	test.EqOp(T, "mine", found.GetUser().GetUsername())

	// The neighbour's user reads as absent rather than as forbidden, which is
	// what it is from here and is the answer that is not an oracle.
	_, err = h.client.GetUser(h.ctx(), &identitypb.GetUserRequest{UserId: theirs.ID})
	must.Error(T, err)
	test.EqOp(T, codes.NotFound, status.Code(err))

	// And the same caller, moved to the other directory, sees the mirror image.
	ctx := h.as(&testPrincipal{userID: "caller", scope: otherScope})

	found, err = h.client.GetUser(ctx, &identitypb.GetUserRequest{UserId: theirs.ID})
	must.NoError(T, err)
	test.EqOp(T, "theirs", found.GetUser().GetUsername())
}

func TestListUsersPagesTheCallersDirectoryOnly(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	h.seedUser(T, testScope, "one")
	h.seedUser(T, testScope, "two")
	h.seedUser(T, otherScope, "elsewhere")

	page, err := h.client.ListUsers(h.ctx(), &identitypb.ListUsersRequest{})
	must.NoError(T, err)

	usernames := make([]string, 0, len(page.GetResults()))
	for _, u := range page.GetResults() {
		usernames = append(usernames, u.GetUsername())
	}

	test.SliceContains(T, usernames, "one")
	test.SliceContains(T, usernames, "two")
	test.SliceNotContains(T, usernames, "elsewhere")
	test.NotNil(T, page.GetPagination(), test.Sprint("a paged read answered with no pagination"))
}

// TestAReadNeverRendersACredential is the end-to-end version of the schema test:
// a user with a password hash in the database reaches a client without it.
func TestAReadNeverRendersACredential(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	user := h.seedUser(T, testScope, "somebody")

	// A real hash in the column, written the way a sign-in flow would write one,
	// so this asserts against a row that actually holds a secret.
	const hash = "$argon2id$v=19$m=65536,t=3,p=2$c29tZXNhbHQ$hunter2"

	must.NoError(T, h.db.WithTransaction(T.Context(), func(tx database.Tx) error {
		return h.store.UpdateUserPassword(T.Context(), tx, testScope, user.ID, hash)
	}))

	found, err := h.client.GetUser(h.ctx(), &identitypb.GetUserRequest{UserId: user.ID})
	must.NoError(T, err)

	test.StrNotContains(T, found.GetUser().String(), "argon2")
	test.StrNotContains(T, found.GetUser().String(), "hunter2")
}

func TestUpdateProfileSavesTheCallersOwnRow(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	registration := h.seedAccount(T, testScope, "somebody")
	ctx := h.as(&testPrincipal{userID: registration.User.ID, scope: testScope})

	response, err := h.client.UpdateProfile(ctx, &identitypb.UpdateProfileRequest{
		Input: &identitypb.ProfileUpdateInput{FirstName: new("Renamed")},
	})
	must.NoError(T, err)

	test.EqOp(T, "Renamed", response.GetUser().GetFirstName())
	test.EqOp(T, registration.User.ID, response.GetUser().GetId())

	// The fields the request did not name are the fields it did not change.
	// This is what optional buys on the wire: a rename that had to resend the
	// username and the email address to avoid blanking them is a client that
	// re-sends stale ones, and a changed email address clears its verification.
	test.EqOp(T, "somebody", response.GetUser().GetUsername())
	test.EqOp(T, "somebody@example.com", response.GetUser().GetEmailAddress())
}

func TestUpdateAccountLeavesWhatTheRequestDidNotName(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	registration := h.seedAccount(T, testScope, "somebody")
	ctx := h.as(&testPrincipal{userID: registration.User.ID, scope: testScope})

	// Give the account something to lose.
	seeded, err := h.client.UpdateAccount(ctx, &identitypb.UpdateAccountRequest{
		AccountId: registration.Account.ID,
		Input: &identitypb.AccountUpdateInput{
			TimeZone:       new("Europe/Amsterdam"),
			BillingAddress: &identitypb.BillingAddress{Line1: "1 Main St", City: "Springfield", Country: "US"},
		},
	})
	must.NoError(T, err)
	test.EqOp(T, "Europe/Amsterdam", seeded.GetAccount().GetTimeZone())

	// A rename, and nothing else on the request. Before presence was on the
	// wire this reset the time zone and blanked the address, and told the hook
	// all three had changed.
	renamed, err := h.client.UpdateAccount(ctx, &identitypb.UpdateAccountRequest{
		AccountId: registration.Account.ID,
		Input:     &identitypb.AccountUpdateInput{Name: new("Renamed")},
	})
	must.NoError(T, err)

	test.EqOp(T, "Renamed", renamed.GetAccount().GetName())
	test.EqOp(T, "Europe/Amsterdam", renamed.GetAccount().GetTimeZone())
	test.EqOp(T, "1 Main St", renamed.GetAccount().GetBillingAddress().GetLine1())
	test.EqOp(T, "Springfield", renamed.GetAccount().GetBillingAddress().GetCity())

	// Present and empty is the other half of the reading: it clears.
	cleared, err := h.client.UpdateAccount(ctx, &identitypb.UpdateAccountRequest{
		AccountId: registration.Account.ID,
		Input: &identitypb.AccountUpdateInput{
			TimeZone:       new(""),
			BillingAddress: &identitypb.BillingAddress{},
		},
	})
	must.NoError(T, err)

	test.EqOp(T, "Renamed", cleared.GetAccount().GetName())
	test.EqOp(T, "", cleared.GetAccount().GetTimeZone())
	test.EqOp(T, "", cleared.GetAccount().GetBillingAddress().GetLine1())
}

// TestAnAnonymousCallClosesWhatItOpened pins the one path that returns before
// the RPC has deferred its own cleanup. The helper every RPC starts with opens
// a span, counts an attempt and starts a latency timer before it looks for a
// principal; if it finds none it has to close all three itself, or every
// unauthenticated request is a span never ended, a histogram with no sample
// and a failure nobody counted.
func TestAnAnonymousCallClosesWhatItOpened(T *testing.T) {
	T.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	T.Cleanup(func() { must.NoError(T, tracerProvider.Shutdown(context.Background())) })

	instruments := &recordingInstruments{}

	h := newHarness(T,
		identitygrpc.WithTracerProvider(tracerProvider),
		identitygrpc.WithMetricsProvider(instruments.provider()),
	)

	_, err := h.client.GetUser(T.Context(), &identitypb.GetUserRequest{UserId: "x"})
	must.Error(T, err)
	test.EqOp(T, codes.Unauthenticated, status.Code(err))

	test.SliceLen(T, 1, recorder.Ended(), test.Sprint("the span was opened and never ended"))
	test.EqOp(T, int64(1), instruments.requests.Load())
	test.EqOp(T, int64(1), instruments.errors.Load(), test.Sprint("the failure was not counted"))
	test.EqOp(T, int64(1), instruments.latencies.Load(), test.Sprint("the timer was started and never stopped"))
}

// recordingInstruments is the request/error/latency trio as three counters, so
// a test can ask whether each was touched. The OperationSet names them by
// suffix, which is how the provider below tells them apart.
type recordingInstruments struct {
	requests  atomic.Int64
	errors    atomic.Int64
	latencies atomic.Int64
}

func (r *recordingInstruments) provider() metrics.Provider {
	return &metricsmock.ProviderMock{
		NewInt64CounterFunc: func(name string, _ ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
			if strings.HasSuffix(name, "_errors") {
				return countingCounter{&r.errors}, nil
			}

			return countingCounter{&r.requests}, nil
		},
		NewFloat64HistogramFunc: func(string, ...metric.Float64HistogramOption) (metrics.Float64Histogram, error) {
			return countingHistogram{&r.latencies}, nil
		},
	}
}

type countingCounter struct{ n *atomic.Int64 }

func (c countingCounter) Add(_ context.Context, incr int64, _ ...metric.AddOption) { c.n.Add(incr) }

type countingHistogram struct{ n *atomic.Int64 }

func (h countingHistogram) Record(context.Context, float64, ...metric.RecordOption) { h.n.Add(1) }

func TestGetPrincipalAnswersForTheCaller(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	registration := h.seedAccount(T, testScope, "somebody")
	ctx := h.as(&testPrincipal{userID: registration.User.ID, scope: testScope})

	response, err := h.client.GetPrincipal(ctx, &identitypb.GetPrincipalRequest{})
	must.NoError(T, err)

	principal := response.GetPrincipal()
	must.NotNil(T, principal)

	test.EqOp(T, registration.User.ID, principal.GetUser().GetId())
	test.EqOp(T, registration.Account.ID, principal.GetActiveAccountId())
	test.SliceLen(T, 1, principal.GetMemberships())
}

func TestArchiveUserRefusesTheLastOwnerOfAnAccount(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	registration := h.seedAccount(T, testScope, "owner")

	_, err := h.client.ArchiveUser(h.ctx(), &identitypb.ArchiveUserRequest{UserId: registration.User.ID})
	must.Error(T, err)

	// FailedPrecondition rather than Internal: the caller can fix this, in a
	// specific order, and the mapping is what tells them so.
	test.EqOp(T, codes.FailedPrecondition, status.Code(err))
	test.True(T, errors.Is(err, identity.ErrLastAccountOwner))
}

func TestUpdateUserAccountStatusRefusesAnUnsetStatus(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	user := h.seedUser(T, testScope, "somebody")

	_, err := h.client.UpdateUserAccountStatus(h.ctx(), &identitypb.UpdateUserAccountStatusRequest{
		UserId: user.ID,
		Status: identitypb.AccountStatus_ACCOUNT_STATUS_UNSPECIFIED,
	})
	must.Error(T, err)

	// InvalidArgument rather than a default of "good", which is the status a
	// client leaving the field unset would most often have meant and the one
	// that would silently reinstate a banned user.
	test.EqOp(T, codes.InvalidArgument, status.Code(err))
}

func TestUpdateUserAccountStatusMovesTheUser(T *testing.T) {
	T.Parallel()

	h := newHarness(T)

	user := h.seedUser(T, testScope, "somebody")

	response, err := h.client.UpdateUserAccountStatus(h.ctx(), &identitypb.UpdateUserAccountStatusRequest{
		UserId:      user.ID,
		Status:      identitypb.AccountStatus_ACCOUNT_STATUS_BANNED,
		Explanation: "spam",
	})
	must.NoError(T, err)

	test.EqOp(T, identitypb.AccountStatus_ACCOUNT_STATUS_BANNED, response.GetUser().GetAccountStatus())
	test.EqOp(T, "spam", response.GetUser().GetAccountStatusExplanation())
}
