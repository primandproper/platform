package dataprivacy

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v10/cryptography/shredding"
	"github.com/primandproper/platform-go/v10/database"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/identifiers"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// recordingShredder stands in for the key store, so these tests are about when
// dataprivacy shreds rather than about how shredding works.
type recordingShredder struct {
	err      error
	subjects []shredding.Subject
	at       atomic.Int64
}

var _ shredding.Shredder = (*recordingShredder)(nil)

// unscopedSubject is the shape a "forget me entirely" request carries. The
// suite's testSubject is confined to one account, which is precisely the case a
// shred cannot serve — see TestWorker_ShredScoped.
var unscopedSubject = Subject{ID: "user-1", Type: SubjectUser}

// runErasureFor saves an erasure for the subject and drives one worker cycle.
func runErasureFor(t *testing.T, env *workerEnv, subject Subject) *Request {
	t.Helper()

	req := saveRequest(t, env.store,
		newRequest(identifiers.New(), RequestErasure, subject, env.clock.read()))

	env.worker.cycle(t.Context())

	read, err := env.store.Get(t.Context(), req.ID)
	must.NoError(t, err)

	return read
}

func (s *recordingShredder) Shred(_ context.Context, subject shredding.Subject) (shredding.Receipt, error) {
	if s.err != nil {
		return shredding.Receipt{}, s.err
	}

	s.subjects = append(s.subjects, subject)
	s.at.Add(1)

	return shredding.Receipt{Subject: subject, ShreddedAt: baseTime, Destroyed: true}, nil
}

func TestWorker_Shred(T *testing.T) {
	T.Parallel()

	T.Run("destroys the subject's key and records when", func(t *testing.T) {
		t.Parallel()

		var ran atomic.Int64

		shredder := &recordingShredder{}
		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterEraser("identity", countingEraser(3, 0, nil, &ran)))
		}, WithWorkerShredder(shredder))

		req := runErasureFor(t, env, unscopedSubject)

		test.EqOp(t, StatusCompleted, req.Status)
		must.NotNil(t, req.KeyShreddedAt)
		test.EqOp(t, baseTime, req.KeyShreddedAt.UTC())

		must.SliceLen(t, 1, shredder.subjects)
		test.EqOp(t, shredding.Subject{Type: string(SubjectUser), ID: testSubject.ID}, shredder.subjects[0])

		// The erasers still run. Shredding covers what the application chose to
		// encrypt per subject; the rows are still the erasers' job.
		test.EqOp(t, int64(1), ran.Load())
	})

	T.Run("does nothing to an export", func(t *testing.T) {
		t.Parallel()

		shredder := &recordingShredder{}
		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"a":1}`)))
		}, WithWorkerShredder(shredder))

		req := env.submitAndRun(t, RequestExport)

		test.EqOp(t, StatusCompleted, req.Status)
		test.Nil(t, req.KeyShreddedAt)
		test.SliceEmpty(t, shredder.subjects)
	})

	T.Run("shreds before the erasers run", func(t *testing.T) {
		t.Parallel()

		var shreddedFirst atomic.Bool

		shredder := &recordingShredder{}
		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterEraser("identity",
				EraserFunc(func(context.Context, database.SQLQueryExecutor, Subject) (ErasureOutcome, error) {
					shreddedFirst.Store(shredder.at.Load() == 1)

					return ErasureOutcome{Deleted: 1}, nil
				})))
		}, WithWorkerShredder(shredder))

		runErasureFor(t, env, unscopedSubject)

		// Erase-then-fail-to-shred would leave the rows gone and every backup
		// readable until a retry succeeded, which is the gap this feature
		// exists to close. Shred-then-fail leaves noise and a retryable delete.
		test.True(t, shreddedFirst.Load())
	})

	T.Run("fails the request when the key cannot be destroyed", func(t *testing.T) {
		t.Parallel()

		var ran atomic.Int64

		shredder := &recordingShredder{err: platformerrors.New("kms unreachable")}
		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterEraser("identity", countingEraser(3, 0, nil, &ran)))
		}, WithWorkerShredder(shredder))

		req := runErasureFor(t, env, unscopedSubject)

		test.EqOp(t, StatusPending, req.Status)
		test.StrContains(t, req.LastError, "kms unreachable")
		test.Nil(t, req.KeyShreddedAt)

		// Nothing was deleted either. An erasure that cannot reach backups is
		// retried whole rather than half-applied.
		test.EqOp(t, int64(0), ran.Load())
	})

	T.Run("records the destruction even when the erasure then fails", func(t *testing.T) {
		t.Parallel()

		shredder := &recordingShredder{}
		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterEraser("identity",
				EraserFunc(func(context.Context, database.SQLQueryExecutor, Subject) (ErasureOutcome, error) {
					return ErasureOutcome{}, platformerrors.New("the ninth domain timed out")
				})))
		}, WithWorkerShredder(shredder))

		req := runErasureFor(t, env, unscopedSubject)

		test.EqOp(t, StatusPending, req.Status)

		// The key is gone whatever happens next, so the row has to say so
		// before the request has finished. Recording it only at completion
		// would leave a subject whose ciphertext is noise and whose rows are
		// still there with nothing anywhere saying why.
		must.NotNil(t, req.KeyShreddedAt)
		test.EqOp(t, baseTime, req.KeyShreddedAt.UTC())
	})

	T.Run("does not move the destruction time on a retry", func(t *testing.T) {
		t.Parallel()

		var attempts atomic.Int64

		shredder := &recordingShredder{}
		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterEraser("identity",
				EraserFunc(func(context.Context, database.SQLQueryExecutor, Subject) (ErasureOutcome, error) {
					if attempts.Add(1) == 1 {
						return ErasureOutcome{}, platformerrors.New("the ninth domain timed out")
					}

					return ErasureOutcome{Deleted: 1}, nil
				})))
		}, WithWorkerShredder(shredder))

		req := runErasureFor(t, env, unscopedSubject)
		must.EqOp(t, StatusPending, req.Status)

		// The retry re-shreds and is told the original destruction time. The
		// column has to keep saying when the key stopped existing, not when
		// somebody last asked about it.
		env.clock.advance(time.Hour)
		env.worker.cycle(t.Context())

		read, err := env.store.Get(t.Context(), req.ID)
		must.NoError(t, err)

		test.EqOp(t, StatusCompleted, read.Status)
		must.NotNil(t, read.KeyShreddedAt)
		test.EqOp(t, baseTime, read.KeyShreddedAt.UTC())
	})

	T.Run("survives a Worker with no shredder", func(t *testing.T) {
		t.Parallel()

		var ran atomic.Int64

		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterEraser("identity", countingEraser(3, 0, nil, &ran)))
		})

		req := runErasureFor(t, env, unscopedSubject)

		test.EqOp(t, StatusCompleted, req.Status)
		test.Nil(t, req.KeyShreddedAt)
		test.EqOp(t, int64(1), ran.Load())
	})
}

func TestWorker_ShredScoped(T *testing.T) {
	T.Parallel()

	T.Run("skips a scoped request and says why", func(t *testing.T) {
		t.Parallel()

		shredder := &recordingShredder{}
		env := newWorkerEnv(t, func(r *Registry) {
			must.NoError(t, r.RegisterEraser("identity", countingEraser(3, 0, nil, nil)))
		}, WithWorkerShredder(shredder))

		// Scope confines an erasure to one tenant; a data key covers every
		// scope its subject appears in. Destroying it would erase that person's
		// data inside tenants nobody asked about.
		read := runErasureFor(t, env, Subject{ID: "user-1", Type: SubjectUser, Scope: "account-1"})

		test.EqOp(t, StatusCompleted, read.Status)
		test.SliceEmpty(t, shredder.subjects)
		test.Nil(t, read.KeyShreddedAt)

		// Not silently: Retained already means "what was kept and on what
		// basis", which is exactly what an unshreddable key is.
		basis, ok := read.Retained[shredRetentionKey]
		must.True(t, ok)
		test.StrContains(t, basis, "one scope")
	})
}
