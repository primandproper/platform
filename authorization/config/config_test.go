package authorizationcfg

import (
	"testing"

	"github.com/primandproper/platform-go/v8/authorization"
	authzdb "github.com/primandproper/platform-go/v8/authorization/database"
	loggingnoop "github.com/primandproper/platform-go/v8/observability/logging/noop"
	metricsnoop "github.com/primandproper/platform-go/v8/observability/metrics/noop"
	tracingnoop "github.com/primandproper/platform-go/v8/observability/tracing/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

const (
	permRead  authorization.Permission = "read.things"
	permWrite authorization.Permission = "write.things"
)

func build(t *testing.T, cfg *Config) (authorization.PolicyResolver, error) {
	t.Helper()

	return NewPolicyResolver(
		t.Context(),
		cfg,
		nil,
		nil,
		loggingnoop.NewLogger(),
		tracingnoop.NewTracerProvider(),
		metricsnoop.NewMetricsProvider(),
	)
}

// The default is what most users get, so "runs with no infrastructure" and
// "denies when unconfigured" are asserted together — neither can regress
// without failing here.
func TestNewPolicyResolver_Default(T *testing.T) {
	T.Parallel()

	T.Run("a zero-value config builds without a database", func(t *testing.T) {
		t.Parallel()

		resolver, err := build(t, &Config{})

		must.NoError(t, err)
		test.NotNil(t, resolver)
	})

	T.Run("a nil config builds", func(t *testing.T) {
		t.Parallel()

		resolver, err := build(t, nil)

		must.NoError(t, err)
		test.NotNil(t, resolver)
	})

	T.Run("the default resolver denies everything", func(t *testing.T) {
		t.Parallel()

		resolver, err := build(t, &Config{})
		must.NoError(t, err)

		set, err := resolver.PermissionsForRoles(t.Context(), "admin")
		must.NoError(t, err)

		test.True(t, set.IsEmpty())
		test.False(t, authorization.NewGrants(set).Has(permRead))
	})

	T.Run("an empty provider selects static", func(t *testing.T) {
		t.Parallel()

		resolver, err := build(t, &Config{
			Roles: []authorization.Role{{Name: "member", Permissions: []authorization.Permission{permRead}}},
		})
		must.NoError(t, err)

		set, err := resolver.PermissionsForRoles(t.Context(), "member")
		must.NoError(t, err)

		test.True(t, set.Has(permRead))
	})
}

func TestNewPolicyResolver_Static(T *testing.T) {
	T.Parallel()

	T.Run("resolves roles loaded from config", func(t *testing.T) {
		t.Parallel()

		resolver, err := build(t, &Config{
			Provider: ProviderStatic,
			Roles: []authorization.Role{
				{Name: "member", Permissions: []authorization.Permission{permRead}},
				{Name: "admin", Permissions: []authorization.Permission{permWrite}, Inherits: []string{"member"}},
			},
		})
		must.NoError(t, err)

		set, err := resolver.PermissionsForRoles(t.Context(), "admin")
		must.NoError(t, err)

		test.True(t, set.HasAll(permRead, permWrite))
	})

	T.Run("rejects a malformed policy at construction", func(t *testing.T) {
		t.Parallel()

		_, err := build(t, &Config{
			Provider: ProviderStatic,
			Roles:    []authorization.Role{{Name: "a", Inherits: []string{"a"}}},
		})

		test.Error(t, err)
	})

	T.Run("tolerates whitespace and case in the provider", func(t *testing.T) {
		t.Parallel()

		resolver, err := build(t, &Config{Provider: "  STATIC "})

		must.NoError(t, err)
		test.NotNil(t, resolver)
	})
}

func TestNewPolicyResolver_Database(T *testing.T) {
	T.Parallel()

	T.Run("rejects a database provider with no config", func(t *testing.T) {
		t.Parallel()

		_, err := build(t, &Config{Provider: ProviderDatabase})

		test.Error(t, err)
	})

	T.Run("rejects a database provider with no executor", func(t *testing.T) {
		t.Parallel()

		_, err := build(t, &Config{
			Provider: ProviderDatabase,
			Database: &authzdb.Config{Dialect: authzdb.DialectSQLite},
		})

		test.Error(t, err)
	})
}

func TestNewPolicyResolver_UnknownProvider(T *testing.T) {
	T.Parallel()

	T.Run("rejects an unrecognized provider", func(t *testing.T) {
		t.Parallel()

		_, err := build(t, &Config{Provider: "openfga"})

		test.Error(t, err)
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("the zero value is valid", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, (&Config{}).ValidateWithContext(t.Context()))
	})

	T.Run("static is valid", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, (&Config{Provider: ProviderStatic}).ValidateWithContext(t.Context()))
	})

	T.Run("database requires its config", func(t *testing.T) {
		t.Parallel()

		test.Error(t, (&Config{Provider: ProviderDatabase}).ValidateWithContext(t.Context()))

		test.NoError(t, (&Config{
			Provider: ProviderDatabase,
			Database: &authzdb.Config{Dialect: authzdb.DialectSQLite},
		}).ValidateWithContext(t.Context()))
	})

	// Mutual exclusion catches the half-migrated config: a database block left
	// behind after switching back to static would otherwise sit there looking
	// authoritative while doing nothing.
	T.Run("static rejects a stray database config", func(t *testing.T) {
		t.Parallel()

		test.Error(t, (&Config{
			Provider: ProviderStatic,
			Database: &authzdb.Config{Dialect: authzdb.DialectSQLite},
		}).ValidateWithContext(t.Context()))
	})

	T.Run("rejects an unknown provider", func(t *testing.T) {
		t.Parallel()

		test.Error(t, (&Config{Provider: "openfga"}).ValidateWithContext(t.Context()))
	})
}
