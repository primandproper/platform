// Package main is not built, imported, or tested. It exists to be handed to
// `go build` by TestWriterIsNotATx, which requires that the build fails.
//
// Go has no negative-compile assertion, and this is the mechanism this module
// picked over the alternative — a documented example and nothing more. An
// example goes stale silently: the day a refactor makes Writer assignable to a
// Tx parameter again, prose still says it does not compile and no job disagrees.
// A build that has to fail is checked by CI on every run.
//
// It lives under testdata because the go tool ignores that directory for
// wildcard patterns, so `go build ./...`, `go vet ./...` and `go test ./...`
// never see this file. Only an explicit path reaches it, which is what the test
// hands to `go build`.
//
// # Why the store and the writer are declared here
//
// They used to be identity.SQLStore and outbox.Writer, which are the real
// components the mistake was found in. They are declared locally now, and the
// reason is the module's tier split: database is a primitive and leaves for
// primitives-go, while identity and outbox own tables and stay. testdata is
// invisible to the go tool, so no module edge was ever created — but a fixture
// that names a package the module it ships with does not have is a fixture that
// stops building for a reason nobody meant.
//
// What is copied is a signature rather than an implementation, and the copy
// cannot drift into a different claim: the whole subject is which of database's
// own types satisfies database.Tx, so the two declarations below say only
// "somebody's write takes a Tx", which is the convention CLAUDE.md states and
// database/convention_test.go pins against the real stores.
//
// The receiver and method names are load-bearing. TestWriterIsNotATx matches
// the compiler's output for "in argument to w.Enqueue" and "in argument to
// store.CreateUser", so that a fixture which stopped naming a real API fails
// the test rather than passing it on an "undefined" error.
package main

import (
	"context"

	"github.com/primandproper/platform-go/v14/database"
)

// message is the shape an outbox write carries: a topic and a payload, whose
// contents are beside the point here.
type message struct {
	Topic string
}

// writer stands in for the outbox writer. What matters is the signature: the
// executor parameter is a database.Tx, so only a caller already inside a
// transaction can reach it.
type writer struct{}

func (*writer) Enqueue(context.Context, database.Tx, message) error { return nil }

// user is the shape a user write carries.
type user struct {
	Username string
}

// store stands in for a store that owns a table — a user row and its role rows
// are one fact, so the write takes the transaction that will carry both.
type store struct{}

func (*store) CreateUser(context.Context, database.Tx, *user) error { return nil }

// enqueueThroughWriter is the mistake outbox/doc.go used to describe in prose.
// A Writer is a SQLQueryExecutor, and Enqueue takes a Tx.
func enqueueThroughWriter(ctx context.Context, client database.Client, w *writer) error {
	return w.Enqueue(ctx, client.Writer(), message{Topic: "orders"})
}

// registerThroughWriter is the identity half: a user row and its role rows as
// two independent autocommits.
func registerThroughWriter(ctx context.Context, client database.Client, store *store) error {
	return store.CreateUser(ctx, client.Writer(), &user{})
}

// readerIsNotATxEither, because a transaction on the read replica is a worse
// version of the same mistake.
func readerIsNotATxEither(ctx context.Context, client database.Client, w *writer) error {
	return w.Enqueue(ctx, client.Reader(), message{Topic: "orders"})
}

func main() {
	_ = enqueueThroughWriter
	_ = registerThroughWriter
	_ = readerIsNotATxEither
}
