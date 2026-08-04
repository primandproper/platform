package cfgnorm

import (
	"testing"

	"github.com/caarlos0/env/v11"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

type leaf struct {
	Token string `env:"TOKEN"`
}

type defaulted struct {
	Provider string `env:"PROVIDER" envDefault:"postgres"`
	Host     string `env:"HOST"`
}

type nested struct {
	Inner *leaf `env:",init" envPrefix:"INNER_"`
	Name  string
}

type root struct {
	Leaf      *leaf      `env:",init"        envPrefix:"LEAF_"`
	Defaulted *defaulted `env:",init"        envPrefix:"DEFAULTED_"`
	Nested    *nested    `env:",init"        envPrefix:"NESTED_"`
	Value     leaf       `envPrefix:"VALUE_"`
	Slice     []string
}

func TestUnconfiguredToNil(T *testing.T) {
	T.Parallel()

	T.Run("releases every sub-config after a parse in an empty environment", func(t *testing.T) {
		t.Parallel()

		cfg := &root{}
		must.NoError(t, env.Parse(cfg))

		// The premise: ",init" leaves all three allocated, and two of them
		// non-zero, so ZeroToNil alone would report them as configured.
		must.NotNil(t, cfg.Leaf)
		must.NotNil(t, cfg.Defaulted)
		must.NotNil(t, cfg.Nested)
		test.EqOp(t, "postgres", cfg.Defaulted.Provider)
		must.NotNil(t, cfg.Nested.Inner)

		must.NoError(t, UnconfiguredToNil(cfg))

		test.Nil(t, cfg.Leaf)
		test.Nil(t, cfg.Defaulted)
		test.Nil(t, cfg.Nested)
	})

	T.Run("keeps a sub-config the environment filled in", func(t *testing.T) {
		t.Parallel()

		cfg := &root{}
		must.NoError(t, env.ParseWithOptions(cfg, env.Options{Environment: map[string]string{"LEAF_TOKEN": "sekret"}}))
		must.NoError(t, UnconfiguredToNil(cfg))

		must.NotNil(t, cfg.Leaf)
		test.EqOp(t, "sekret", cfg.Leaf.Token)
		test.Nil(t, cfg.Defaulted)
	})

	T.Run("keeps a sub-config that only departs from a default", func(t *testing.T) {
		t.Parallel()

		cfg := &root{}
		must.NoError(t, env.ParseWithOptions(cfg, env.Options{Environment: map[string]string{"DEFAULTED_PROVIDER": "mysql"}}))
		must.NoError(t, UnconfiguredToNil(cfg))

		must.NotNil(t, cfg.Defaulted)
		test.EqOp(t, "mysql", cfg.Defaulted.Provider)
	})

	T.Run("keeps a sub-config filled in by assignment rather than parsing", func(t *testing.T) {
		t.Parallel()

		cfg := &root{Leaf: &leaf{Token: "assigned"}}
		must.NoError(t, UnconfiguredToNil(cfg))

		must.NotNil(t, cfg.Leaf)
		test.EqOp(t, "assigned", cfg.Leaf.Token)
	})

	T.Run("releases a block spelling out nothing but the defaults", func(t *testing.T) {
		t.Parallel()

		// The documented edge: writing provider=postgres, which is what the
		// type parses to anyway, configures nothing and is released.
		cfg := &root{Defaulted: &defaulted{Provider: "postgres"}}
		must.NoError(t, UnconfiguredToNil(cfg))

		test.Nil(t, cfg.Defaulted)
	})

	T.Run("leaves non-pointer and non-struct fields alone", func(t *testing.T) {
		t.Parallel()

		cfg := &root{Value: leaf{Token: "kept"}, Slice: []string{"a"}}
		must.NoError(t, UnconfiguredToNil(cfg))

		test.EqOp(t, "kept", cfg.Value.Token)
		test.SliceLen(t, 1, cfg.Slice)
	})

	T.Run("rejects anything but a non-nil pointer to a struct", func(t *testing.T) {
		t.Parallel()

		test.Error(t, UnconfiguredToNil(root{}))
		test.Error(t, UnconfiguredToNil((*root)(nil)))
		test.Error(t, UnconfiguredToNil(nil))
	})
}
