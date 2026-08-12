package outboxemit

import (
	"context"
	"slices"
	"sync"

	"github.com/primandproper/platform-go/v10/database"
	databasemock "github.com/primandproper/platform-go/v10/database/mock"
	"github.com/primandproper/platform-go/v10/outbox"
)

// dataChange stands in for the consumer's message type. This package never
// looks inside one, so its contents matter only to the assertions.
type dataChange struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// otherMessage is a second message type, for the one assertion that needs two:
// that a side effect written against the wrong one is refused.
type otherMessage struct {
	Whatever string `json:"whatever"`
}

// recordingEnqueuer captures the argument list of every Enqueue.
//
// That list is what an Emitter's contract is made of — which messages one call
// produces, and how many calls it takes to produce them — so the tests read it
// directly rather than inferring it from rows in a database.
type recordingEnqueuer struct {
	err   error
	calls [][]outbox.Message
	mu    sync.Mutex
}

var _ Enqueuer = (*recordingEnqueuer)(nil)

func (r *recordingEnqueuer) Enqueue(_ context.Context, _ database.SQLQueryExecutor, msgs ...outbox.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, slices.Clone(msgs))

	return r.err
}

// recorded returns the messages of the only Enqueue made, and reports how many
// were made, so a caller can assert both at once.
func (r *recordingEnqueuer) recorded() (msgs []outbox.Message, calls int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.calls) == 0 {
		return nil, 0
	}

	return r.calls[0], len(r.calls)
}

// testExecutor is a SQLQueryExecutor that nothing in these tests calls. The
// Emitter only passes it along, so the moq zero value is right: a method
// reached by accident panics rather than quietly returning a zero.
func testExecutor() database.SQLQueryExecutor {
	return &databasemock.SQLQueryExecutorMock{}
}
