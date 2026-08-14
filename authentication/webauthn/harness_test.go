package webauthn

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	loggingnoop "github.com/primandproper/platform-go/v10/observability/logging/noop"
	metricsnoop "github.com/primandproper/platform-go/v10/observability/metrics/noop"
	tracingnoop "github.com/primandproper/platform-go/v10/observability/tracing/noop"

	"github.com/shoenig/test/must"
)

const (
	testRPID   = "example.com"
	testOrigin = "https://example.com"
)

// testUser is the twenty-line adapter this package expects an application to
// write, written once here so the ceremonies have somebody to run for.
type testUser struct {
	name        string
	handle      []byte
	credentials []Credential
}

var _ User = (*testUser)(nil)

func newTestUser(handle string) *testUser {
	return &testUser{name: handle, handle: []byte(handle)}
}

func (u *testUser) WebAuthnID() []byte                { return u.handle }
func (u *testUser) WebAuthnName() string              { return u.name }
func (u *testUser) WebAuthnDisplayName() string       { return u.name }
func (u *testUser) WebAuthnCredentials() []Credential { return u.credentials }
func (u *testUser) add(credential *Credential)        { u.credentials = append(u.credentials, *credential) }

// memoryStore is a SessionStore in a map: the store this package refuses to
// ship, kept here because a unit test of the ceremonies is a single process and
// the ceremonies are what these tests are about.
//
// It is deliberately not a shortcut anybody can reach for — the two real
// implementations live in subpackages, and both are held to
// webauthntest.Run.
type memoryStore struct {
	sessions map[string]*SessionData

	saveErr    error
	consumeErr error

	mu sync.Mutex
}

var _ SessionStore = (*memoryStore)(nil)

func newMemoryStore() *memoryStore {
	return &memoryStore{sessions: map[string]*SessionData{}}
}

func (s *memoryStore) Save(_ context.Context, session *SessionData, ttl time.Duration) error {
	if s.saveErr != nil {
		return s.saveErr
	}

	if err := ValidateSession(session, ttl); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored := *session
	s.sessions[session.Challenge] = &stored

	return nil
}

func (s *memoryStore) Consume(_ context.Context, challenge string) (*SessionData, error) {
	if s.consumeErr != nil {
		return nil, s.consumeErr
	}

	if challenge == "" {
		return nil, ErrChallengeRequired
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[challenge]
	if !ok {
		return nil, ErrSessionNotFound
	}

	delete(s.sessions, challenge)

	return session, nil
}

// count reports how many ceremonies are outstanding, which is what a Begin
// changes and a caller cannot see.
func (s *memoryStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.sessions)
}

// newTestRelyingParty builds a relying party over a store the test can inspect.
func newTestRelyingParty(tb testing.TB, store SessionStore, opts ...Option) *RelyingParty {
	tb.Helper()

	rp, err := NewRelyingParty(tb.Context(), &Config{
		RPID:          testRPID,
		RPDisplayName: "Example",
		RPOrigins:     []string{testOrigin},
	}, store, append([]Option{
		WithLogger(loggingnoop.NewLogger()),
		WithTracerProvider(tracingnoop.NewTracerProvider()),
		WithMetricsProvider(metricsnoop.NewMetricsProvider()),
	}, opts...)...)
	must.NoError(tb, err)

	return rp
}

// post wraps a ceremony response in the request a browser would send, which is
// what the *http.Request entry points parse.
func post(tb testing.TB, body []byte) *http.Request {
	tb.Helper()

	req := httptest.NewRequest(http.MethodPost, "/webauthn", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	return req
}
