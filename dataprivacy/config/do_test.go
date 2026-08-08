package dataprivacycfg

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/primandproper/platform-go/v10/database"
	databasecfg "github.com/primandproper/platform-go/v10/database/config"
	"github.com/primandproper/platform-go/v10/database/dialect"
	"github.com/primandproper/platform-go/v10/dataprivacy"
	"github.com/primandproper/platform-go/v10/uploads"
	uploadsnoop "github.com/primandproper/platform-go/v10/uploads/noop"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func testDBClient(t *testing.T) database.Client {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	client, err := databasecfg.NewDatabase(t.Context(), &databasecfg.Config{
		Provider:        databasecfg.ProviderSQLite,
		ReadConnection:  databasecfg.ConnectionDetails{Database: path},
		WriteConnection: databasecfg.ConnectionDetails{Database: path},
	}, nil)
	must.NoError(t, err)

	return client
}

func testConfig() *Config {
	return &Config{Dialect: dialect.SQLite}
}

func TestRegisterStore(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue[database.Client](i, testDBClient(t))
		do.ProvideValue(i, testConfig())

		RegisterStore(i)

		store, err := do.Invoke[dataprivacy.Store](i)
		must.NoError(t, err)
		test.NotNil(t, store)
	})
}

func TestRegisterService(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue[database.Client](i, testDBClient(t))
		do.ProvideValue(i, testConfig())

		RegisterStore(i)
		RegisterService(i)

		service, err := do.Invoke[dataprivacy.Service](i)
		must.NoError(t, err)
		test.NotNil(t, service)
	})
}

func TestRegisterWorker(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue[database.Client](i, testDBClient(t))
		do.ProvideValue(i, testConfig())

		registry := dataprivacy.NewRegistry()
		must.NoError(t, registry.RegisterCollector("example", dataprivacy.CollectorFunc(
			func(context.Context, dataprivacy.Subject) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			},
		)))
		do.ProvideValue(i, registry)
		do.ProvideValue[uploads.UploadManager](i, uploadsnoop.NewUploadManager())

		RegisterStore(i)
		RegisterWorker(i)

		worker, err := do.Invoke[*dataprivacy.Worker](i)
		must.NoError(t, err)
		test.NotNil(t, worker)
	})
}

func TestRegisterSweeper(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue[database.Client](i, testDBClient(t))
		do.ProvideValue(i, testConfig())
		do.ProvideValue[uploads.UploadManager](i, uploadsnoop.NewUploadManager())

		RegisterStore(i)
		RegisterSweeper(i)

		sweeper, err := do.Invoke[*dataprivacy.Sweeper](i)
		must.NoError(t, err)
		test.NotNil(t, sweeper)
	})
}
