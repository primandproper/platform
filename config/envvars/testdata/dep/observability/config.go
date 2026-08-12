// Package observability is a fixture whose configuration refers to another
// package of its own module, which is what makes an intra-dependency import
// resolvable or not.
package observability

import db "example.com/dep/database"

// Config nests one struct of its own and one from a sibling package.
type Config struct {
	Logging LoggingConfig `envPrefix:"LOGGING_"`
	Audit   db.Connection `envPrefix:"AUDIT_"`
}

// LoggingConfig is declared in this package.
type LoggingConfig struct {
	Level string `env:"LEVEL" envDefault:"info"`
}
