package database

import "context"

type Manager interface {
	CreateUser(ctx context.Context, username, password string) error
	DeleteUser(ctx context.Context, username string) error

	CreateDatabase(ctx context.Context, dbName, owner string) error
	DeleteDatabase(ctx context.Context, dbName string) error

	UserExists(ctx context.Context, username string) (bool, error)
	DatabaseExists(ctx context.Context, dbName string) (bool, error)

	GrantUserAccessToTable(ctx context.Context, username, schema, table, privilege string) error
	UserCanAccessDatabase(ctx context.Context, username, dbName string) (bool, error)
}
