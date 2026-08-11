package healthcheck

import (
	"context"

	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/errors"
)

// DatabaseReadyChecker checks if a database client is ready.
type DatabaseReadyChecker interface {
	IsReady(ctx context.Context) bool
}

// NewDatabaseChecker returns a Checker that uses the given client's IsReady method.
func NewDatabaseChecker(name string, client DatabaseReadyChecker) Checker {
	return &databaseChecker{name: name, client: client}
}

type databaseChecker struct {
	client DatabaseReadyChecker
	name   string
}

func (d *databaseChecker) Name() string {
	return d.name
}

func (d *databaseChecker) Check(ctx context.Context) error {
	if d.client == nil {
		return errors.New("database client is nil")
	}
	if !d.client.IsReady(ctx) {
		return database.ErrDatabaseNotReady
	}
	return nil
}

// Pinger is anything whose readiness is a Ping. Cache clients and message queue
// clients both are, and so is most of what a service depends on over a
// connection.
type Pinger interface {
	Ping(ctx context.Context) error
}

// CacheReadyChecker checks if a cache client is ready.
type CacheReadyChecker = Pinger

// MessageQueueReadyChecker checks if a message queue client is ready.
type MessageQueueReadyChecker = Pinger

// NewCacheChecker returns a Checker that pings the given cache client.
func NewCacheChecker(name string, client CacheReadyChecker) Checker {
	return NewPingChecker(name, "cache", client)
}

// NewMessageQueueChecker returns a Checker that pings the given message queue
// client.
func NewMessageQueueChecker(name string, client MessageQueueReadyChecker) Checker {
	return NewPingChecker(name, "message queue", client)
}

// NewPingChecker returns a Checker that reports whatever client.Ping says.
//
// name is the dependency's name in the probe's output; subject names the kind of
// client in the error a nil one produces, which is the only place the two
// wrappers above ever differed. They existed as separate types because the
// packages they check are separate, not because the check is — and a third
// pingable dependency would have been a third byte-identical copy.
//
// A nil client is an error rather than a pass. A probe reporting healthy for a
// dependency it was never given is worse than no probe: it is a green check
// covering a wiring mistake.
func NewPingChecker(name, subject string, client Pinger) Checker {
	return &pingChecker{name: name, subject: subject, client: client}
}

type pingChecker struct {
	client  Pinger
	name    string
	subject string
}

func (p *pingChecker) Name() string {
	return p.name
}

func (p *pingChecker) Check(ctx context.Context) error {
	if p.client == nil {
		return errors.Newf("%s client is nil", p.subject)
	}

	return p.client.Ping(ctx)
}
