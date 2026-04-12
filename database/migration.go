package database

import (
	"context"
	"database/sql"
	"time"
)

// Migrator is an interface for running database migrations.
// Implementations handle the specifics of migration execution (e.g., darwin, goose, etc.)
type Migrator interface {
	Migrate(ctx context.Context, db *sql.DB) error
}

// ClientConfig provides the configuration needed by database clients.
// This interface allows the config package to provide configuration
// without creating an import cycle.
type ClientConfig interface {
	GetReadConnectionString() string
	GetWriteConnectionString() string
	GetMaxPingAttempts() uint64
	GetPingWaitPeriod() time.Duration
	GetMaxIdleConns() int
	GetMaxOpenConns() int
	GetConnMaxLifetime() time.Duration
}
