package outboxemit_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/primandproper/platform-go/v10/database"
	databasemock "github.com/primandproper/platform-go/v10/database/mock"
	"github.com/primandproper/platform-go/v10/outbox"
	outboxemit "github.com/primandproper/platform-go/v10/outbox/emit"
	searchsync "github.com/primandproper/platform-go/v10/search/sync"
)

// settingChanged is the consumer's message type. outboxemit never looks inside
// one; the outbox marshals it, and pins JSON.
type settingChanged struct {
	Type      string `json:"type"`
	SettingID string `json:"settingID"`
	AccountID string `json:"accountID"`
}

// printingWriter stands in for the outbox.Writer a deployment passes, so the
// example can show what one Emit actually produced.
type printingWriter struct{}

func (printingWriter) Enqueue(_ context.Context, _ database.SQLQueryExecutor, msgs ...outbox.Message) error {
	fmt.Printf("one Enqueue, %d messages:\n", len(msgs))

	for i := range msgs {
		switch payload := msgs[i].Payload.(type) {
		case searchsync.Event:
			fmt.Printf("  topic=%s key=%s index %s of %s\n", msgs[i].Topic, msgs[i].Key, payload.Op, payload.DocumentID)
		default:
			fmt.Printf("  topic=%s key=%s payload=%T\n", msgs[i].Topic, msgs[i].Key, payload)
		}
	}

	return nil
}

// exampleExecutor stands in for the executor database.Client.WithTransaction
// hands its callback. The side effect below writes through it.
func exampleExecutor() database.SQLQueryExecutor {
	return &databasemock.SQLQueryExecutorMock{
		ExecContextFunc: func(context.Context, string, ...any) (sql.Result, error) {
			return driver.RowsAffected(1), nil
		},
	}
}

// dispatchWebhooks is the consumer-owned half: what an endpoint subscription
// looks like, and what a dispatch row holds, are the application's entirely. It
// runs on the caller's executor, so its rows commit with the change that
// occasioned them.
func dispatchWebhooks(ctx context.Context, q database.SQLQueryExecutor, msg settingChanged) ([]outbox.Message, error) {
	if _, err := q.ExecContext(ctx, "INSERT INTO webhook_dispatches ...", msg.AccountID, msg.Type); err != nil {
		return nil, err
	}

	fmt.Println("wrote a webhook dispatch row")

	return nil, nil
}

// One write owes three things here: a data-change message, a search index event
// so the index does not go stale, and a webhook dispatch row. All three belong
// in the transaction that changed the row, and one Emit is how none of them
// gets forgotten.
func ExampleEmitter_Emit() {
	emitter, err := outboxemit.NewEmitter[settingChanged]("data_changes", printingWriter{},
		outboxemit.WithSideEffect("webhooks", dispatchWebhooks))
	if err != nil {
		fmt.Println(err)

		return
	}

	// In a real service this is the body of a database.Client.WithTransaction
	// callback, after the statement that changed the row.
	err = emitter.Emit(context.Background(), exampleExecutor(),
		settingChanged{Type: "setting.updated", SettingID: "setting-1", AccountID: "account-7"},
		outboxemit.WithIndexUpsert("settings-index", "setting-1"),
		outboxemit.WithOrderingKey("setting-1"),
	)
	if err != nil {
		fmt.Println(err)

		return
	}

	// Output:
	// wrote a webhook dispatch row
	// one Enqueue, 2 messages:
	//   topic=data_changes key=setting-1 payload=outboxemit_test.settingChanged
	//   topic=settings-index key=setting-1 index upsert of setting-1
}
