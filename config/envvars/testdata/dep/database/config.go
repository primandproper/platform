// Package database is a fixture standing in for a library's configuration
// package, which a service's own configuration is assembled out of.
package database

// Base is embedded, and its fields are promoted into whatever embeds it.
type Base struct {
	Host string `env:"HOST" envDefault:"localhost"`
}

// Config is the struct a service nests under a prefix of its own choosing.
type Config struct {
	Base

	ConnectionDetails Connection `envPrefix:"CONNECTION_"`
	Debug             bool       `env:"DEBUG"`
}

// Connection is nested a second level down, and is also referred to from
// another package of this same module.
type Connection struct {
	Port int `env:"PORT" envDefault:"5432"`
}
