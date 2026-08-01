package dataprivacy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v9/clock"
	clockmock "github.com/primandproper/platform-go/v9/clock/mock"
	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/database/dialect"
	"github.com/primandproper/platform-go/v9/database/sqlite"
	"github.com/primandproper/platform-go/v9/dataprivacy/migrations"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/uploads"

	"github.com/shoenig/test/must"
)

// baseTime is the instant this suite works relative to.
var baseTime = time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)

// testClientConfig is the minimum database.ClientConfig a SQLite client needs.
type testClientConfig struct {
	connectionString string
}

var _ database.ClientConfig = (*testClientConfig)(nil)

func (c *testClientConfig) GetReadConnectionString() string   { return c.connectionString }
func (c *testClientConfig) GetWriteConnectionString() string  { return c.connectionString }
func (c *testClientConfig) GetMaxPingAttempts() uint64        { return 1 }
func (c *testClientConfig) GetPingWaitPeriod() time.Duration  { return time.Millisecond }
func (c *testClientConfig) GetMaxIdleConns() int              { return 2 }
func (c *testClientConfig) GetMaxOpenConns() int              { return 1 }
func (c *testClientConfig) GetConnMaxLifetime() time.Duration { return time.Minute }

// stubClock is a manually advanced clock. Expiry, deadlines, and confirmation
// windows are all functions of elapsed time and these tests need days of it, so
// they control the clock rather than race the wall.
//
// A synctest bubble would normally spare us a double, but it advances fake time
// only once every goroutine in the bubble is durably blocked, and these tests
// drive a real SQLite file. Built on the generated mock so the methods nothing
// calls fail loudly instead of lying.
type stubClock struct {
	*clockmock.ClockMock

	now time.Time
	mu  sync.Mutex
}

var _ clock.Clock = (*stubClock)(nil)

func newStubClock() *stubClock {
	c := &stubClock{now: baseTime}

	c.ClockMock = &clockmock.ClockMock{
		NowFunc:       c.read,
		SinceFunc:     func(t time.Time) time.Duration { return c.read().Sub(t) },
		NewTickerFunc: clock.NewClock().NewTicker,
	}

	return c
}

func (c *stubClock) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *stubClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

// prefixCounter names a fresh table per subtest. Subtests share one database
// and must not share tables — the claim predicate is global to the requests
// table, so one test's backlog would be another's.
var prefixCounter atomic.Uint64

// storeEnv is one live database plus the dialect to emit SQL for.
type storeEnv struct {
	client  database.Client
	dialect dialect.Dialect
}

// newSQLiteEnv builds a SQLite-backed environment. SQLite exercises the real
// SQL — placeholder rendering, the claim predicate, the guarded transitions,
// the partial indexes — without a container.
func newSQLiteEnv(t *testing.T) *storeEnv {
	t.Helper()

	client, err := sqlite.NewDatabaseClient(t.Context(),
		&testClientConfig{connectionString: filepath.Join(t.TempDir(), "dataprivacy.db")})
	must.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return &storeEnv{client: client, dialect: dialect.SQLite}
}

// newStore migrates a uniquely prefixed request table and returns a Store over
// it.
func (e *storeEnv) newStore(t *testing.T) Store {
	t.Helper()

	prefix := fmt.Sprintf("dp_%d", prefixCounter.Add(1))

	stmts, err := migrations.Statements(e.dialect, prefix)
	must.NoError(t, err)
	must.SliceNotEmpty(t, stmts)

	for _, stmt := range stmts {
		_, execErr := e.client.Writer().ExecContext(t.Context(), stmt)
		must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
	}

	store, err := NewSQLStore(e.dialect, e.client, WithTablePrefix(prefix))
	must.NoError(t, err)

	return store
}

// saveRequest inserts a request through a transaction, as the Service does.
func saveRequest(t *testing.T, store Store, req *Request) *Request {
	t.Helper()

	must.NoError(t, store.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
		return store.Save(t.Context(), q, req)
	}))

	return req
}

// newRequest builds a pending request of the given type.
func newRequest(id string, t RequestType, subject Subject, at time.Time) *Request {
	return &Request{
		ID:          id,
		Type:        t,
		Subject:     subject,
		Status:      StatusPending,
		RequestedAt: at,
		DueAt:       at.Add(DefaultResponseWindow),
	}
}

// testSubject is the subject most of this suite is about.
var testSubject = Subject{ID: "user-1", Type: SubjectUser, Scope: "account-1"}

// memoryUploader is an in-process UploadManager. It records what was written so
// a test can assert the artifact's bytes, and implements Delete/Exists so the
// sweeper's already-gone path is reachable.
type memoryUploader struct {
	objects map[string][]byte
	types   map[string]string

	deleteErr error

	mu sync.Mutex
}

var _ uploads.UploadManager = (*memoryUploader)(nil)

func newMemoryUploader() *memoryUploader {
	return &memoryUploader{
		objects: map[string][]byte{},
		types:   map[string]string{},
	}
}

func (m *memoryUploader) Save(_ context.Context, path string, r io.Reader, opts ...uploads.SaveOption) error {
	content, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.objects[path] = content
	m.types[path] = uploads.BuildSaveOptions(opts...).ContentType

	return nil
}

func (m *memoryUploader) Open(_ context.Context, path string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	content, ok := m.objects[path]
	if !ok {
		return nil, platformerrors.Newf("no such object %q", path)
	}

	return io.NopCloser(bytes.NewReader(content)), nil
}

func (m *memoryUploader) Delete(_ context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.deleteErr != nil {
		return m.deleteErr
	}

	if _, ok := m.objects[path]; !ok {
		return platformerrors.Newf("no such object %q", path)
	}

	delete(m.objects, path)

	return nil
}

func (m *memoryUploader) Exists(_ context.Context, path string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.objects[path]

	return ok, nil
}

func (m *memoryUploader) get(path string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	content, ok := m.objects[path]

	return content, ok
}

func (m *memoryUploader) paths() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Sorted(maps.Keys(m.objects))
}

// signingUploader is a memoryUploader that can also sign, for the Download
// path. Kept separate so a test can exercise the "provider cannot sign" branch
// by using the plain one.
type signingUploader struct {
	*memoryUploader
}

var (
	_ uploads.UploadManager = (*signingUploader)(nil)
	_ uploads.URLSigner     = (*signingUploader)(nil)
)

func (s *signingUploader) SignedURL(_ context.Context, path string, opts *uploads.SignedURLOptions) (string, error) {
	expiry := time.Duration(0)
	if opts != nil {
		expiry = opts.Expiry
	}

	return fmt.Sprintf("https://storage.example/%s?expires_in=%s", path, expiry), nil
}

// staticCollector returns fixed bytes.
func staticCollector(fragment string) Collector {
	return CollectorFunc(func(context.Context, Subject) (json.RawMessage, error) {
		return json.RawMessage(fragment), nil
	})
}

// failingCollector always errors.
func failingCollector(err error) Collector {
	return CollectorFunc(func(context.Context, Subject) (json.RawMessage, error) {
		return nil, err
	})
}

// countingEraser reports a fixed outcome and records that it ran.
func countingEraser(deleted, anonymized int64, retained map[string]string, ran *atomic.Int64) Eraser {
	return EraserFunc(func(context.Context, database.SQLQueryExecutor, Subject) (ErasureOutcome, error) {
		if ran != nil {
			ran.Add(1)
		}

		return ErasureOutcome{Deleted: deleted, Anonymized: anonymized, Retained: retained}, nil
	})
}

// failingClaimStore fails every Claim, so a cycle's error path is reachable.
// Embedding the real Store means only the one method under test is a double.
type failingClaimStore struct {
	Store
}

func (s *failingClaimStore) Claim(context.Context, time.Time, int, time.Time) ([]*Request, error) {
	return nil, platformerrors.New("the database is unreachable")
}

// failingOverdueStore fails only the overdue count, so a sweep's other chores
// still run and the partial result is observable.
type failingOverdueStore struct {
	Store
}

func (s *failingOverdueStore) CountOverdue(context.Context, time.Time) (map[RequestType]int64, error) {
	return nil, platformerrors.New("the read replica is unreachable")
}

// stringReader is a small convenience for writing fixture objects.
func stringReader(content string) io.Reader {
	return bytes.NewReader([]byte(content))
}

// decodeArtifact reads a stored artifact back into a Document.
func decodeArtifact(t *testing.T, p *packager, stored []byte) *Document {
	t.Helper()

	decoded, err := p.decode(t.Context(), stored)
	must.NoError(t, err)

	var doc Document
	must.NoError(t, json.Unmarshal(decoded, &doc))

	return &doc
}
