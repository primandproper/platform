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
package main

import (
	"context"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/identity"
	"github.com/primandproper/platform-go/v13/outbox"
)

// enqueueThroughWriter is the mistake outbox/doc.go used to describe in prose.
// A Writer is a SQLQueryExecutor, and Enqueue now takes a Tx.
func enqueueThroughWriter(ctx context.Context, client database.Client, w *outbox.Writer) error {
	return w.Enqueue(ctx, client.Writer(), outbox.Message{Topic: "orders"})
}

// registerThroughWriter is the identity half: a user row and its role rows as
// two independent autocommits.
func registerThroughWriter(ctx context.Context, client database.Client, store *identity.SQLStore) error {
	return store.CreateUser(ctx, client.Writer(), &identity.User{})
}

// readerIsNotATxEither, because a transaction on the read replica is a worse
// version of the same mistake.
func readerIsNotATxEither(ctx context.Context, client database.Client, w *outbox.Writer) error {
	return w.Enqueue(ctx, client.Reader(), outbox.Message{Topic: "orders"})
}

func main() {
	_ = enqueueThroughWriter
	_ = registerThroughWriter
	_ = readerIsNotATxEither
}
