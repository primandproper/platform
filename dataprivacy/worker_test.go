package dataprivacy

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v10/compression"
	"github.com/primandproper/platform-go/v10/database"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/identifiers"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// workerEnv is a Worker wired over a live store and an in-memory bucket.
type workerEnv struct {
	store    Store
	worker   *Worker
	uploader *memoryUploader
	registry *Registry
	clock    *stubClock
}

// newWorkerEnv builds a Worker with the given registrations.
func newWorkerEnv(t *testing.T, register func(*Registry), opts ...WorkerOption) *workerEnv {
	t.Helper()

	env := newSQLiteEnv(t)
	store := env.newStore(t)
	uploader := newMemoryUploader()
	registry := NewRegistry()
	stub := newStubClock()

	register(registry)

	base := []WorkerOption{
		WithWorkerUploadManager(uploader),
		WithWorkerClock(stub),
	}

	worker, err := NewWorker(t.Context(), &WorkerConfig{}, store, registry, append(base, opts...)...)
	must.NoError(t, err)

	return &workerEnv{store: store, worker: worker, uploader: uploader, registry: registry, clock: stub}
}

// submitAndRun saves a request, then drives one worker cycle over it.
func (e *workerEnv) submitAndRun(t *testing.T, requestType RequestType) *Request {
	t.Helper()

	req := saveRequest(t, e.store, newRequest(identifiers.New(), requestType, testSubject, e.clock.read()))

	e.worker.cycle(t.Context())

	read, err := e.store.Get(t.Context(), req.ID)
	must.NoError(t, err)

	return read
}

func TestWorker_Export(T *testing.T) {
	T.Parallel()

	T.Run("assembles every section into the artifact", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"email":"a@example.com"}`)))
			must.NoError(t, r.RegisterCollector("billing", staticCollector(`{"invoices":2}`)))
		})

		req := env.submitAndRun(t, RequestExport)

		test.EqOp(t, StatusCompleted, req.Status)
		test.False(t, req.Partial())
		must.StrNotEqFold(t, "", req.ArtifactRef)
		test.Greater(t, int64(0), req.ArtifactBytes)

		stored, ok := env.uploader.get(req.ArtifactRef)
		must.True(t, ok)

		doc := decodeArtifact(t, &env.worker.packager, stored)

		test.EqOp(t, DocumentFormat, doc.Manifest.Format)
		test.EqOp(t, req.ID, doc.Manifest.RequestID)
		test.EqOp(t, testSubject.ID, doc.Manifest.Subject.ID)
		test.Eq(t, []string{"billing", "identity"}, doc.Manifest.Sections)
		test.MapLen(t, 2, doc.Data)
		test.MapEmpty(t, doc.Manifest.Failures)

		// The fragment reaches the artifact as the collector returned it.
		test.EqOp(t, `{"invoices":2}`, string(mustCompact(t, doc.Data["billing"])))
	})

	T.Run("a failing collector costs its section, not the export", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"email":"a@example.com"}`)))
			must.NoError(t, r.RegisterCollector("billing", failingCollector(platformerrors.New("billing is down"))))
		})

		req := env.submitAndRun(t, RequestExport)

		// Delivered, not failed. A subject with thirty days to complain is
		// better served by most of their data plus an honest account of the gap.
		test.EqOp(t, StatusCompleted, req.Status)
		test.True(t, req.Partial())
		must.MapLen(t, 1, req.Failures)
		test.StrContains(t, req.Failures["billing"], "billing is down")

		stored, ok := env.uploader.get(req.ArtifactRef)
		must.True(t, ok)

		doc := decodeArtifact(t, &env.worker.packager, stored)

		// The manifest names the gap, so the document does not silently assert
		// that the missing data does not exist.
		test.Eq(t, []string{"identity"}, doc.Manifest.Sections)
		must.MapLen(t, 1, doc.Manifest.Failures)
		test.StrContains(t, doc.Manifest.Failures["billing"], "billing is down")
	})

	T.Run("an export where every collector failed is a hard failure", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", failingCollector(platformerrors.New("nope"))))
			must.NoError(t, r.RegisterCollector("billing", failingCollector(platformerrors.New("nope"))))
		})

		req := env.submitAndRun(t, RequestExport)

		// A file asserting that nothing is held about a person is the one wrong
		// answer available here, so nothing is written at all.
		test.EqOp(t, StatusPending, req.Status)
		test.StrContains(t, req.LastError, "no dataprivacy collector succeeded")
		test.SliceEmpty(t, env.uploader.paths())
	})

	T.Run("a collector returning nothing omits its section", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"email":"a@example.com"}`)))
			must.NoError(t, r.RegisterCollector("billing", CollectorFunc(
				func(context.Context, Subject) (json.RawMessage, error) { return nil, nil },
			)))
		})

		req := env.submitAndRun(t, RequestExport)

		test.EqOp(t, StatusCompleted, req.Status)
		test.False(t, req.Partial())

		stored, _ := env.uploader.get(req.ArtifactRef)
		doc := decodeArtifact(t, &env.worker.packager, stored)

		// "Nothing about this subject" is a complete answer, and omitting the
		// section says so. Writing null would claim the domain holds a null.
		test.Eq(t, []string{"identity"}, doc.Manifest.Sections)
		test.MapLen(t, 1, doc.Data)
	})

	T.Run("a collector panic becomes that section's failure", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"ok":true}`)))
			must.NoError(t, r.RegisterCollector("billing", CollectorFunc(
				func(context.Context, Subject) (json.RawMessage, error) { panic("boom") },
			)))
		})

		req := env.submitAndRun(t, RequestExport)

		// Somebody else's code running in our goroutine: a nil map access in
		// one domain costs that domain's section, not the whole batch.
		test.EqOp(t, StatusCompleted, req.Status)
		must.MapLen(t, 1, req.Failures)
		test.StrContains(t, req.Failures["billing"], "panicked")
	})

	T.Run("a collector returning malformed JSON fails only its section", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"ok":true}`)))
			must.NoError(t, r.RegisterCollector("billing", staticCollector(`{"broken":`)))
		})

		req := env.submitAndRun(t, RequestExport)

		// Caught before assembly. A malformed fragment reaching the artifact
		// would make the whole document unparseable — one domain's bug becoming
		// a total loss.
		test.EqOp(t, StatusCompleted, req.Status)
		must.MapLen(t, 1, req.Failures)
		test.StrContains(t, req.Failures["billing"], "invalid JSON")

		stored, _ := env.uploader.get(req.ArtifactRef)

		var doc Document
		decoded, err := env.worker.packager.decode(t.Context(), stored)
		must.NoError(t, err)
		test.NoError(t, json.Unmarshal(decoded, &doc))
	})

	T.Run("the artifact is stored uncacheable", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"ok":true}`)))
		})

		req := env.submitAndRun(t, RequestExport)

		// A cache between the bucket and the subject would keep serving the
		// object after the link expired and after the sweeper deleted it.
		env.uploader.mu.Lock()
		defer env.uploader.mu.Unlock()

		test.EqOp(t, "application/json", env.uploader.types[req.ArtifactRef])
	})

	T.Run("compression round trips", func(t *testing.T) {
		t.Parallel()

		compressor, err := compression.NewCompressor(compression.AlgorithmZstd)
		must.NoError(t, err)

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"email":"a@example.com"}`)))
		}, WithWorkerCompressor(compressor))

		req := env.submitAndRun(t, RequestExport)

		stored, ok := env.uploader.get(req.ArtifactRef)
		must.True(t, ok)

		doc := decodeArtifact(t, &env.worker.packager, stored)
		test.Eq(t, []string{"identity"}, doc.Manifest.Sections)
	})

	T.Run("an oversized document fails without being stored", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"padding":"aaaaaaaaaaaaaaaaaaaa"}`)))
		})

		env.worker.cfg.MaxDocumentBytes = 8

		req := env.submitAndRun(t, RequestExport)

		// Unretryable: it will be the same size next time.
		test.EqOp(t, StatusFailed, req.Status)
		test.StrContains(t, req.LastError, "exceeds configured maximum")
		test.SliceEmpty(t, env.uploader.paths())
	})
}

func TestWorker_Erasure(T *testing.T) {
	T.Parallel()

	T.Run("sums outcomes and namespaces retentions", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterEraser("identity",
				countingEraser(5, 1, nil, nil)))
			must.NoError(t, r.RegisterEraser("billing",
				countingEraser(2, 3, map[string]string{"invoices": "tax law"}, nil)))
		})

		req := env.submitAndRun(t, RequestErasure)

		test.EqOp(t, StatusCompleted, req.Status)
		test.EqOp(t, int64(7), req.Deleted)
		test.EqOp(t, int64(4), req.Anonymized)

		// Namespaced by eraser key so two domains retaining "invoices" for
		// different reasons do not overwrite each other.
		must.MapLen(t, 1, req.Retained)
		test.EqOp(t, "tax law", req.Retained["billing.invoices"])
	})

	T.Run("one eraser failing rolls the whole erasure back", func(t *testing.T) {
		t.Parallel()

		var ranFirst atomic.Int64

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterEraser("aaa", countingEraser(5, 0, nil, &ranFirst)))
			must.NoError(t, r.RegisterEraser("zzz", EraserFunc(
				func(context.Context, database.SQLQueryExecutor, Subject) (ErasureOutcome, error) {
					return ErasureOutcome{}, platformerrors.New("cannot reach billing")
				},
			)))
		})

		req := env.submitAndRun(t, RequestErasure)

		// A subject deleted from one domain and present in another has not been
		// erased and cannot be told they have.
		test.EqOp(t, StatusPending, req.Status)
		test.StrContains(t, req.LastError, "cannot reach billing")
		test.EqOp(t, int64(0), req.Deleted)
		test.EqOp(t, int64(0), req.Anonymized)

		// The first eraser did run — its work is undone by the rollback, not by
		// never having happened.
		test.EqOp(t, int64(1), ranFirst.Load())
	})

	T.Run("an eraser panic aborts the erasure", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterEraser("identity", EraserFunc(
				func(context.Context, database.SQLQueryExecutor, Subject) (ErasureOutcome, error) {
					panic("boom")
				},
			)))
		})

		req := env.submitAndRun(t, RequestErasure)

		test.EqOp(t, StatusPending, req.Status)
		test.StrContains(t, req.LastError, "panicked")
	})

	T.Run("an erasure with no registered eraser fails terminally", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", staticCollector(`{}`)))
		})

		req := env.submitAndRun(t, RequestErasure)

		// An erasure that erases nothing while reporting success is the worst
		// failure available here, because nobody goes looking for it.
		test.EqOp(t, StatusFailed, req.Status)
		test.StrContains(t, req.LastError, "no dataprivacy erasers registered")
	})
}

func TestWorker_Retries(T *testing.T) {
	T.Parallel()

	T.Run("gives up once attempts are exhausted", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", failingCollector(platformerrors.New("still down"))))
		})

		req := saveRequest(t, env.store, newRequest(identifiers.New(), RequestExport, testSubject, env.clock.read()))

		maxAttempts := int(env.worker.cfg.Backoff.MaxAttempts)

		for range maxAttempts {
			env.worker.cycle(t.Context())

			// Backoff is persisted as a timestamp rather than slept through, so
			// the clock has to move for the next claim to see the request.
			env.clock.advance(time.Hour)
		}

		read, err := env.store.Get(t.Context(), req.ID)
		must.NoError(t, err)

		test.EqOp(t, StatusFailed, read.Status)
		test.EqOp(t, maxAttempts, read.Attempts)
	})

	T.Run("notifies the subject when a request fails permanently", func(t *testing.T) {
		t.Parallel()

		var (
			notified   atomic.Int64
			lastStatus atomic.Pointer[Status]
		)

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", failingCollector(platformerrors.New("down"))))
		}, WithWorkerNotifier(NotifierFunc(func(_ context.Context, n *Notification) error {
			notified.Add(1)
			lastStatus.Store(&n.Request.Status)

			return nil
		})))

		saveRequest(t, env.store, newRequest(identifiers.New(), RequestExport, testSubject, env.clock.read()))

		for range int(env.worker.cfg.Backoff.MaxAttempts) {
			env.worker.cycle(t.Context())
			env.clock.advance(time.Hour)
		}

		// Somebody is owed an answer and is not going to get one. Telling them
		// beats a status page that says "processing" until the window runs out.
		test.EqOp(t, int64(1), notified.Load())
		must.NotNil(t, lastStatus.Load())
		test.EqOp(t, StatusFailed, *lastStatus.Load())
	})

	T.Run("a notification failure does not fail the request", func(t *testing.T) {
		t.Parallel()

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"ok":true}`)))
		}, WithWorkerNotifier(NotifierFunc(func(context.Context, *Notification) error {
			return platformerrors.New("mail server is down")
		})))

		req := env.submitAndRun(t, RequestExport)

		// The export exists; re-running it to retry an email would re-run every
		// collector against the subject's data.
		test.EqOp(t, StatusCompleted, req.Status)
	})
}

func TestNewArtifactURLSigner(T *testing.T) {
	T.Parallel()

	T.Run("signs a completed export's artifact", func(t *testing.T) {
		t.Parallel()

		uploader := &signingUploader{memoryUploader: newMemoryUploader()}

		sign := NewArtifactURLSigner(uploader, time.Minute, false)

		url, expiresAt := sign(t.Context(), &Request{ArtifactRef: "exports/x.json"})

		test.StrContains(t, url, "exports/x.json")
		test.False(t, expiresAt.IsZero())
	})

	T.Run("declines when artifacts are encrypted", func(t *testing.T) {
		t.Parallel()

		uploader := &signingUploader{memoryUploader: newMemoryUploader()}

		// The same refusal Service.Download makes, for the same reason: the
		// subject would receive base64 ciphertext.
		sign := NewArtifactURLSigner(uploader, time.Minute, true)

		url, expiresAt := sign(t.Context(), &Request{ArtifactRef: "exports/x.json"})

		test.EqOp(t, "", url)
		test.True(t, expiresAt.IsZero())
	})

	T.Run("declines when the provider cannot sign", func(t *testing.T) {
		t.Parallel()

		sign := NewArtifactURLSigner(newMemoryUploader(), time.Minute, false)

		url, _ := sign(t.Context(), &Request{ArtifactRef: "exports/x.json"})
		test.EqOp(t, "", url)
	})

	T.Run("declines a request with no artifact", func(t *testing.T) {
		t.Parallel()

		uploader := &signingUploader{memoryUploader: newMemoryUploader()}

		sign := NewArtifactURLSigner(uploader, time.Minute, false)

		url, _ := sign(t.Context(), &Request{})
		test.EqOp(t, "", url)
	})

	T.Run("reaches the notification", func(t *testing.T) {
		t.Parallel()

		uploader := &signingUploader{memoryUploader: newMemoryUploader()}

		var deliveredURL atomic.Pointer[string]

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"ok":true}`)))
		},
			WithWorkerUploadManager(uploader),
			WithWorkerURLSigner(NewArtifactURLSigner(uploader, time.Minute, false)),
			WithWorkerNotifier(NotifierFunc(func(_ context.Context, n *Notification) error {
				deliveredURL.Store(&n.DownloadURL)

				return nil
			})),
		)

		req := env.submitAndRun(t, RequestExport)

		must.NotNil(t, deliveredURL.Load())
		test.StrContains(t, *deliveredURL.Load(), req.ArtifactRef)
	})
}

func TestNewWorker(T *testing.T) {
	T.Parallel()

	T.Run("refuses an empty registry", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		_, err := NewWorker(t.Context(), &WorkerConfig{}, env.newStore(t), NewRegistry(),
			WithWorkerUploadManager(newMemoryUploader()))
		test.ErrorIs(t, err, ErrNoCollectors)
	})

	T.Run("refuses collectors with no storage", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		registry := NewRegistry()
		must.NoError(t, registry.RegisterCollector("identity", staticCollector(`{}`)))

		// A worker that collects eleven domains and then discovers it has
		// nowhere to write has already done all the expensive work.
		_, err := NewWorker(t.Context(), &WorkerConfig{}, env.newStore(t), registry)
		test.ErrorIs(t, err, ErrNoUploadManager)
	})

	T.Run("an erasure-only worker needs no storage", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		registry := NewRegistry()
		must.NoError(t, registry.RegisterEraser("identity", countingEraser(0, 0, nil, nil)))

		_, err := NewWorker(t.Context(), &WorkerConfig{}, env.newStore(t), registry)
		test.NoError(t, err)
	})

	T.Run("refuses a lease that does not outlast the fulfillment timeout", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		registry := NewRegistry()
		must.NoError(t, registry.RegisterEraser("identity", countingEraser(0, 0, nil, nil)))

		// Two workers running the same erasure concurrently means two sets of
		// deletes racing inside two transactions.
		_, err := NewWorker(t.Context(), &WorkerConfig{
			FulfillmentTimeout: time.Hour,
			LeaseDuration:      time.Minute,
		}, env.newStore(t), registry)

		must.Error(t, err)
		test.StrContains(t, err.Error(), "must exceed fulfillment timeout")
	})
}

// mustCompact normalizes JSON for comparison.
func mustCompact(t *testing.T, raw json.RawMessage) []byte {
	t.Helper()

	compacted, err := json.Marshal(raw)
	must.NoError(t, err)

	return compacted
}
