package registry_test

import (
	"context"
	"io"
	"strings"
	"testing"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/tenancy"
	"github.com/primandproper/platform-go/v13/uploads"
	uploadsmock "github.com/primandproper/platform-go/v13/uploads/mock"
	"github.com/primandproper/platform-go/v13/uploads/registry"
	registrymock "github.com/primandproper/platform-go/v13/uploads/registry/mock"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// drainingManager is the UploadManager the happy paths run against: it reads
// the reader to the end, which is what a real provider does and what makes the
// byte count mean anything.
func drainingManager(t *testing.T, stored *[]byte, opts *uploads.SaveOptions) *uploadsmock.UploadManagerMock {
	t.Helper()

	return &uploadsmock.UploadManagerMock{
		SaveFunc: func(_ context.Context, _ string, r io.Reader, saveOpts ...uploads.SaveOption) error {
			read, err := io.ReadAll(r)
			if err != nil {
				return err
			}

			*stored = read
			*opts = uploads.BuildSaveOptions(saveOpts...)

			return nil
		},
	}
}

func TestStoreAndRecord(T *testing.T) {
	T.Parallel()

	newObj := func() *registry.Object {
		return &registry.Object{
			Scope:       tenancy.Of("tenant_1"),
			Key:         "avatars/grace/original.png",
			ContentType: "image/png",
			OwnerID:     "user_1",
		}
	}

	T.Run("stores the bytes and records what was stored", func(t *testing.T) {
		t.Parallel()

		var (
			stored []byte
			opts   uploads.SaveOptions
		)

		manager := drainingManager(t, &stored, &opts)

		var recorded *registry.Object

		store := &registrymock.StoreMock{
			RecordObjectFunc: func(_ context.Context, object *registry.Object) error {
				recorded = object

				return nil
			},
		}

		object := newObj()
		content := "not really a png"

		must.NoError(t, registry.StoreAndRecord(t.Context(), manager, store, object, strings.NewReader(content)))

		test.EqOp(t, content, string(stored))
		must.NotNil(t, recorded)
		test.EqOp(t, object, recorded)

		// The size is what went past, not what anybody claimed.
		test.EqOp(t, int64(len(content)), object.Size)

		// The content type reaches the provider too, so the stored object and
		// its row agree about what it is.
		test.EqOp(t, "image/png", opts.ContentType)
	})

	T.Run("leaves the content type to the provider when the row does not state one", func(t *testing.T) {
		t.Parallel()

		var (
			stored []byte
			opts   uploads.SaveOptions
		)

		manager := drainingManager(t, &stored, &opts)
		store := &registrymock.StoreMock{
			RecordObjectFunc: func(context.Context, *registry.Object) error { return nil },
		}

		object := newObj()
		object.ContentType = ""

		must.NoError(t, registry.StoreAndRecord(t.Context(), manager, store, object, strings.NewReader("x")))

		// Naming it explicitly as "" would replace a sniffed answer with none.
		test.EqOp(t, "", opts.ContentType)
	})

	T.Run("passes the caller's save options through", func(t *testing.T) {
		t.Parallel()

		var (
			stored []byte
			opts   uploads.SaveOptions
		)

		manager := drainingManager(t, &stored, &opts)
		store := &registrymock.StoreMock{
			RecordObjectFunc: func(context.Context, *registry.Object) error { return nil },
		}

		must.NoError(t, registry.StoreAndRecord(t.Context(), manager, store, newObj(),
			strings.NewReader("x"), uploads.WithCacheControl("max-age=3600")))

		test.EqOp(t, "max-age=3600", opts.CacheControl)
	})

	T.Run("does not record when the bytes did not land", func(t *testing.T) {
		t.Parallel()

		saveErr := platformerrors.New("bucket unreachable")

		manager := &uploadsmock.UploadManagerMock{
			SaveFunc: func(context.Context, string, io.Reader, ...uploads.SaveOption) error { return saveErr },
		}

		// RecordObjectFunc is left nil on purpose: the generated mock panics if
		// it is called, which is the assertion. A row pointing at bytes that
		// are not there is the failure this order exists to prevent.
		store := &registrymock.StoreMock{}

		must.ErrorIs(t, registry.StoreAndRecord(t.Context(), manager, store, newObj(), strings.NewReader("x")), saveErr)
	})

	T.Run("reports what the registration reported", func(t *testing.T) {
		t.Parallel()

		var (
			stored []byte
			opts   uploads.SaveOptions
		)

		manager := drainingManager(t, &stored, &opts)
		store := &registrymock.StoreMock{
			RecordObjectFunc: func(context.Context, *registry.Object) error { return registry.ErrObjectKeyTaken },
		}

		// The bytes are in the bucket and the row is not: an object with no
		// row, which is invisible to every read and exactly what an orphan
		// sweep is later written to find.
		must.ErrorIs(t, registry.StoreAndRecord(t.Context(), manager, store, newObj(), strings.NewReader("x")), registry.ErrObjectKeyTaken)
	})

	T.Run("refuses the pieces it cannot do without", func(t *testing.T) {
		t.Parallel()

		manager := &uploadsmock.UploadManagerMock{}
		store := &registrymock.StoreMock{}

		must.ErrorIs(t, registry.StoreAndRecord(t.Context(), nil, store, newObj(), strings.NewReader("x")), registry.ErrNilUploadManager)
		must.ErrorIs(t, registry.StoreAndRecord(t.Context(), manager, nil, newObj(), strings.NewReader("x")), registry.ErrNilStore)
		must.ErrorIs(t, registry.StoreAndRecord(t.Context(), manager, store, nil, strings.NewReader("x")), registry.ErrNilObject)
		must.ErrorIs(t, registry.StoreAndRecord(t.Context(), manager, store, newObj(), nil), registry.ErrNilReader)
	})
}
