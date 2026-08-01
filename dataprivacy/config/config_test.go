package dataprivacycfg

import (
	"testing"

	"github.com/primandproper/platform-go/v9/audit"
	"github.com/primandproper/platform-go/v9/database/dialect"
	"github.com/primandproper/platform-go/v9/dataprivacy"
	"github.com/primandproper/platform-go/v9/dataprivacy/auditerasure"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestConfig(T *testing.T) {
	T.Parallel()

	T.Run("fills defaults", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Dialect: dialect.Postgres}
		cfg.EnsureDefaults()

		test.EqOp(t, dataprivacy.DefaultTablePrefix, cfg.TablePrefix)
		test.EqOp(t, audit.DefaultTablePrefix, cfg.AuditErasure.TablePrefix)
		test.EqOp(t, auditerasure.DefaultRetentionBasis, cfg.AuditErasure.RetentionBasis)

		// The audit eraser is registered unless an operator turns it off: an
		// erasure that silently skipped a store of personal data would be the
		// more surprising default.
		test.False(t, cfg.AuditErasure.Disabled)

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("requires a valid dialect", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Dialect: dialect.Dialect("oracle")}
		cfg.EnsureDefaults()

		// ozzo collects field errors into a map, which does not forward
		// errors.Is to the causes underneath — so this asserts on the rendering.
		err := cfg.ValidateWithContext(t.Context())
		must.Error(t, err)
		test.StrContains(t, err.Error(), "unsupported SQL dialect")
	})

	T.Run("validates the nested configs", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Dialect: dialect.Postgres}
		cfg.EnsureDefaults()

		// ozzo dereferences a struct-value field before checking
		// ValidatableWithContext, so these are reached through By closures — a
		// regression here would silently stop validating them.
		cfg.Worker.LeaseDuration = cfg.Worker.FulfillmentTimeout

		err := cfg.ValidateWithContext(t.Context())
		must.Error(t, err)
		test.StrContains(t, err.Error(), "must exceed fulfillment timeout")
	})
}

func TestRegisterAuditEraser(T *testing.T) {
	T.Parallel()

	T.Run("registers by default", func(t *testing.T) {
		t.Parallel()

		registry := dataprivacy.NewRegistry()

		registered, err := RegisterAuditEraser(t.Context(), &Config{Dialect: dialect.SQLite}, registry)
		must.NoError(t, err)

		test.True(t, registered)
		test.Eq(t, []string{auditerasure.DefaultKey}, registry.EraserKeys())
	})

	T.Run("Disabled leaves the audit log untouched", func(t *testing.T) {
		t.Parallel()

		registry := dataprivacy.NewRegistry()

		cfg := &Config{Dialect: dialect.SQLite}
		cfg.AuditErasure.Disabled = true

		registered, err := RegisterAuditEraser(t.Context(), cfg, registry)
		must.NoError(t, err)

		test.False(t, registered)
		test.SliceEmpty(t, registry.EraserKeys())
	})

	T.Run("refuses a nil registry", func(t *testing.T) {
		t.Parallel()

		_, err := RegisterAuditEraser(t.Context(), &Config{Dialect: dialect.SQLite}, nil)
		test.Error(t, err)
	})

	T.Run("propagates a bad audit table prefix", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Dialect: dialect.SQLite}
		cfg.AuditErasure.TablePrefix = "drop table;--"

		_, err := RegisterAuditEraser(t.Context(), cfg, dataprivacy.NewRegistry())
		test.ErrorIs(t, err, auditerasure.ErrInvalidTablePrefix)
	})
}

func TestEnsurePackaging(T *testing.T) {
	T.Parallel()

	T.Run("supplies nothing when nothing is configured", func(t *testing.T) {
		t.Parallel()

		workerOpts, serviceOpts := EnsurePackaging(nil, nil)

		test.SliceEmpty(t, workerOpts)
		test.SliceEmpty(t, serviceOpts)
	})
}

func TestConstructors(T *testing.T) {
	T.Parallel()

	T.Run("refuse a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewStore(t.Context(), nil, nil)
		test.Error(t, err)

		_, err = NewService(t.Context(), nil, nil, nil, nil, nil)
		test.Error(t, err)

		_, err = NewWorker(t.Context(), nil, nil, nil, nil, nil, nil, nil, false)
		test.Error(t, err)

		_, err = NewSweeper(t.Context(), nil, nil, nil, nil, nil, nil)
		test.Error(t, err)
	})
}
