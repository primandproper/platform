package dataprivacy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v10/compression"
	"github.com/primandproper/platform-go/v10/cryptography/encryption/aes"
	"github.com/primandproper/platform-go/v10/identifiers"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// newTestService builds a Service over a live store and the stub clock.
func newTestService(t *testing.T, cfg *ServiceConfig, opts ...ServiceOption) (Service, Store, *stubClock) {
	t.Helper()

	env := newSQLiteEnv(t)
	store := env.newStore(t)
	stub := newStubClock()

	svc, err := NewService(t.Context(), cfg, store, append([]ServiceOption{WithServiceClock(stub)}, opts...)...)
	must.NoError(t, err)

	return svc, store, stub
}

func TestService_Submit(T *testing.T) {
	T.Parallel()

	T.Run("an export is queued immediately", func(t *testing.T) {
		t.Parallel()

		svc, _, _ := newTestService(t, &ServiceConfig{})

		req, err := svc.Submit(t.Context(), testSubject, RequestExport)
		must.NoError(t, err)

		test.EqOp(t, StatusPending, req.Status)
		test.EqOp(t, RequestExport, req.Type)
		test.True(t, req.RequestedAt.Equal(baseTime))
		test.True(t, req.DueAt.Equal(baseTime.Add(DefaultResponseWindow)))
	})

	T.Run("an erasure is queued when no confirmation window is set", func(t *testing.T) {
		t.Parallel()

		svc, _, _ := newTestService(t, &ServiceConfig{})

		req, err := svc.Submit(t.Context(), testSubject, RequestErasure)
		must.NoError(t, err)

		// Zero window is the default; Confirm is never needed.
		test.EqOp(t, StatusPending, req.Status)
		test.True(t, req.ExpiresAt.IsZero())
	})

	T.Run("an erasure waits for confirmation when a window is set", func(t *testing.T) {
		t.Parallel()

		svc, _, _ := newTestService(t, &ServiceConfig{ConfirmationWindow: 72 * time.Hour})

		req, err := svc.Submit(t.Context(), testSubject, RequestErasure)
		must.NoError(t, err)

		test.EqOp(t, StatusAwaitingConfirmation, req.Status)
		test.True(t, req.ExpiresAt.Equal(baseTime.Add(72*time.Hour)))
	})

	T.Run("an export is never held for confirmation", func(t *testing.T) {
		t.Parallel()

		svc, _, _ := newTestService(t, &ServiceConfig{ConfirmationWindow: 72 * time.Hour})

		// An export is reversible in the only sense that matters — it can be
		// expired and re-run — so confirming one costs a round trip and buys
		// nothing.
		req, err := svc.Submit(t.Context(), testSubject, RequestExport)
		must.NoError(t, err)

		test.EqOp(t, StatusPending, req.Status)
	})

	T.Run("the response window differs per request type", func(t *testing.T) {
		t.Parallel()

		svc, _, _ := newTestService(t, &ServiceConfig{
			ExportResponseWindow:  30 * 24 * time.Hour,
			ErasureResponseWindow: 45 * 24 * time.Hour,
		})

		export, err := svc.Submit(t.Context(), testSubject, RequestExport)
		must.NoError(t, err)

		erasure, err := svc.Submit(t.Context(), testSubject, RequestErasure)
		must.NoError(t, err)

		test.True(t, export.DueAt.Equal(baseTime.Add(30*24*time.Hour)))
		test.True(t, erasure.DueAt.Equal(baseTime.Add(45*24*time.Hour)))
	})

	T.Run("rejects an empty subject", func(t *testing.T) {
		t.Parallel()

		svc, _, _ := newTestService(t, &ServiceConfig{})

		// A request about nobody would fan out over every collector asking for
		// the empty string's data — which some of them will answer.
		_, err := svc.Submit(t.Context(), Subject{}, RequestExport)
		test.ErrorIs(t, err, ErrEmptySubjectID)
	})

	T.Run("rejects an unknown request type", func(t *testing.T) {
		t.Parallel()

		svc, _, _ := newTestService(t, &ServiceConfig{})

		_, err := svc.Submit(t.Context(), testSubject, RequestType("rectification"))
		test.ErrorIs(t, err, ErrUnknownRequestType)
	})
}

func TestService_Confirmation(T *testing.T) {
	T.Parallel()

	T.Run("confirm queues the erasure", func(t *testing.T) {
		t.Parallel()

		svc, _, _ := newTestService(t, &ServiceConfig{ConfirmationWindow: 72 * time.Hour})

		submitted, err := svc.Submit(t.Context(), testSubject, RequestErasure)
		must.NoError(t, err)

		confirmed, err := svc.Confirm(t.Context(), submitted.ID)
		must.NoError(t, err)

		test.EqOp(t, StatusPending, confirmed.Status)
	})

	T.Run("cancel withdraws the erasure", func(t *testing.T) {
		t.Parallel()

		svc, _, _ := newTestService(t, &ServiceConfig{ConfirmationWindow: 72 * time.Hour})

		submitted, err := svc.Submit(t.Context(), testSubject, RequestErasure)
		must.NoError(t, err)

		cancelled, err := svc.Cancel(t.Context(), submitted.ID)
		must.NoError(t, err)

		test.EqOp(t, StatusCancelled, cancelled.Status)
	})

	T.Run("confirming twice is refused", func(t *testing.T) {
		t.Parallel()

		svc, _, _ := newTestService(t, &ServiceConfig{ConfirmationWindow: 72 * time.Hour})

		submitted, err := svc.Submit(t.Context(), testSubject, RequestErasure)
		must.NoError(t, err)

		_, err = svc.Confirm(t.Context(), submitted.ID)
		must.NoError(t, err)

		// The guard is in the predicate rather than a read-then-write, so a
		// subject clicking twice cannot queue the erasure twice.
		_, err = svc.Confirm(t.Context(), submitted.ID)
		test.ErrorIs(t, err, ErrNotAwaitingConfirmation)
	})

	T.Run("confirming a request that was never two-phase is refused", func(t *testing.T) {
		t.Parallel()

		svc, _, _ := newTestService(t, &ServiceConfig{})

		submitted, err := svc.Submit(t.Context(), testSubject, RequestErasure)
		must.NoError(t, err)

		_, err = svc.Confirm(t.Context(), submitted.ID)
		test.ErrorIs(t, err, ErrNotAwaitingConfirmation)
	})
}

func TestService_Artifacts(T *testing.T) {
	T.Parallel()

	// completedExport wires a Service over a store holding one completed export
	// whose artifact is already in the bucket.
	completedExport := func(t *testing.T, uploader *memoryUploader, opts ...ServiceOption) (Service, *Request) {
		t.Helper()

		env := newSQLiteEnv(t)
		store := env.newStore(t)
		stub := newStubClock()

		base := []ServiceOption{WithServiceClock(stub), WithServiceUploadManager(uploader)}

		svc, err := NewService(t.Context(), &ServiceConfig{}, store, append(base, opts...)...)
		must.NoError(t, err)

		req := newRequest(identifiers.New(), RequestExport, testSubject, baseTime)
		req.Status = StatusCompleted
		req.ArtifactRef = "dataprivacy/exports/" + req.ID + ".json"
		req.ExpiresAt = baseTime.Add(DefaultArtifactTTL)
		saveRequest(t, store, req)

		return svc, req
	}

	T.Run("download mints a signed URL", func(t *testing.T) {
		t.Parallel()

		uploader := &signingUploader{memoryUploader: newMemoryUploader()}

		svc, req := completedExport(t, uploader.memoryUploader, WithServiceUploadManager(uploader))

		must.NoError(t, uploader.Save(t.Context(), req.ArtifactRef, strings.NewReader(`{"data":{}}`)))

		url, err := svc.Download(t.Context(), req.ID)
		must.NoError(t, err)

		test.StrContains(t, url, req.ArtifactRef)
		test.StrContains(t, url, DefaultSignedURLTTL.String())
	})

	T.Run("download refuses when artifacts are encrypted", func(t *testing.T) {
		t.Parallel()

		uploader := &signingUploader{memoryUploader: newMemoryUploader()}

		encryptor, err := aes.NewEncryptorDecryptor([]byte("0123456789abcdef0123456789abcdef"))
		must.NoError(t, err)

		svc, req := completedExport(t, uploader.memoryUploader,
			WithServiceUploadManager(uploader),
			WithServiceDecryptor(encryptor),
		)

		// A subject following that link gets base64 ciphertext and finds out
		// some days into a statutory window.
		_, err = svc.Download(t.Context(), req.ID)
		test.ErrorIs(t, err, ErrArtifactEncrypted)
	})

	T.Run("download refuses a provider that cannot sign", func(t *testing.T) {
		t.Parallel()

		uploader := newMemoryUploader()

		svc, req := completedExport(t, uploader)

		_, err := svc.Download(t.Context(), req.ID)
		test.ErrorIs(t, err, ErrNoURLSigner)
	})

	T.Run("open reverses compression and encryption", func(t *testing.T) {
		t.Parallel()

		compressor, err := compression.NewCompressor(compression.AlgorithmZstd)
		must.NoError(t, err)

		encryptor, err := aes.NewEncryptorDecryptor([]byte("0123456789abcdef0123456789abcdef"))
		must.NoError(t, err)

		uploader := newMemoryUploader()

		svc, req := completedExport(t, uploader,
			WithServiceCompressor(compressor),
			WithServiceDecryptor(encryptor),
		)

		// Write through the same pipeline the Worker would.
		writer := &packager{compressor: compressor, encryptor: encryptor}

		stored, err := writer.encode(t.Context(), &Document{
			Data:     map[string]json.RawMessage{"identity": json.RawMessage(`{"email":"a@example.com"}`)},
			Manifest: Manifest{Format: DocumentFormat, RequestID: req.ID},
		})
		must.NoError(t, err)

		must.NoError(t, uploader.Save(t.Context(), req.ArtifactRef, bytes.NewReader(stored)))

		reader, err := svc.Open(t.Context(), req.ID)
		must.NoError(t, err)

		defer func() { _ = reader.Close() }()

		content, err := io.ReadAll(reader)
		must.NoError(t, err)

		test.StrContains(t, string(content), DocumentFormat)
	})

	T.Run("an expired export has no artifact", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)
		store := env.newStore(t)

		svc, err := NewService(t.Context(), &ServiceConfig{}, store,
			WithServiceUploadManager(newMemoryUploader()))
		must.NoError(t, err)

		req := newRequest(identifiers.New(), RequestExport, testSubject, baseTime)
		req.Status = StatusExpired
		saveRequest(t, store, req)

		_, err = svc.Download(t.Context(), req.ID)
		test.ErrorIs(t, err, ErrArtifactUnavailable)

		_, err = svc.Open(t.Context(), req.ID)
		test.ErrorIs(t, err, ErrArtifactUnavailable)
	})

	T.Run("an erasure has no artifact", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)
		store := env.newStore(t)

		svc, err := NewService(t.Context(), &ServiceConfig{}, store,
			WithServiceUploadManager(newMemoryUploader()))
		must.NoError(t, err)

		req := newRequest(identifiers.New(), RequestErasure, testSubject, baseTime)
		req.Status = StatusCompleted
		saveRequest(t, store, req)

		_, err = svc.Open(t.Context(), req.ID)
		test.ErrorIs(t, err, ErrArtifactUnavailable)
	})
}

func TestService_Get(T *testing.T) {
	T.Parallel()

	T.Run("reports a missing request", func(t *testing.T) {
		t.Parallel()

		svc, _, _ := newTestService(t, &ServiceConfig{})

		_, err := svc.Get(t.Context(), "nope")
		test.True(t, errors.Is(err, ErrRequestNotFound))
	})
}

func TestNewService(T *testing.T) {
	T.Parallel()

	T.Run("refuses a nil store", func(t *testing.T) {
		t.Parallel()

		_, err := NewService(t.Context(), &ServiceConfig{}, nil)
		test.ErrorIs(t, err, ErrNilStore)
	})

	T.Run("refuses a nil config", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		_, err := NewService(t.Context(), nil, env.newStore(t))
		test.Error(t, err)
	})
}
