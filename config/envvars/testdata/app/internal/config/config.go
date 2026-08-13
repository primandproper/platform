// Package config is a fixture standing in for a service's configuration
// package: a constraint naming the loadable configuration structs, and structs
// assembled out of both its own types and a dependency's.
package config

import (
	"example.com/dep/database"
	"example.com/dep/observability"
)

// configurations is the constraint every loadable configuration struct is a
// member of, and the thing Options.UnionKey names.
type configurations interface {
	APIServiceConfig | WorkerConfig
}

// APIServiceConfig exercises every shape the walk has an opinion about.
type APIServiceConfig struct {
	Debug         bool                 `env:"DEBUG"         envDefault:"false"`
	Name          string               `env:"NAME"`
	Database      database.Config      `envPrefix:"DATABASE_"`
	Observability observability.Config `envPrefix:"OBSERVABILITY_"`
	Analytics     Analytics            `envPrefix:"ANALYTICS_"`
	Server        *ServerConfig        `envPrefix:"SERVER_"`
	Inline        struct {
		Token string `env:"TOKEN"`
	} `envPrefix:"INLINE_"`

	// Unprefixed carries no envPrefix, and its tags are read as they are
	// written.
	Unprefixed Unprefixed

	// Chain refers to itself, and is where the walk has to stop.
	Chain Chain `envPrefix:"CHAIN_"`

	// Tenants is populated from indexed variables, whose count is not a
	// property of this source.
	Tenants []TenantConfig `envPrefix:"TENANT"`

	// secret cannot be set by the parser, and Ignored is told not to be.
	secret  string `env:"SECRET"`
	Ignored string `env:"-"`
}

// WorkerConfig shares Database with APIServiceConfig, and declares one variable
// of its own whose default contains a comma.
type WorkerConfig struct {
	Database database.Config `envPrefix:"DATABASE_"`
	Queue    string          `env:"QUEUE_NAME" envDefault:"a,b,c"`
}

// Analytics nests a vendor-specific struct one level deeper.
type Analytics struct {
	Posthog PosthogConfig `envPrefix:"POSTHOG_"`
}

// PosthogConfig names a variable whose casing runs through an initialism.
type PosthogConfig struct {
	APIKey string `env:"API_KEY"`
}

// ServerConfig is reached through a pointer.
type ServerConfig struct {
	HTTPPort int `env:"HTTP_PORT" envDefault:"8000"`
}

// Unprefixed declares an empty default, which is not the same as no default.
type Unprefixed struct {
	Region string `env:"REGION" envDefault:""`
}

// Chain is self-referential.
type Chain struct {
	Label string `env:"LABEL"`
	Next  *Chain `envPrefix:"NEXT_"`
}

// TenantConfig is only ever reached through a slice.
type TenantConfig struct {
	Slug string `env:"SLUG"`
}
