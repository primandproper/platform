package healthcheck

import (
	"context"
	"errors"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

var errStub = errors.New("stub error")

func TestNewDatabaseChecker(T *testing.T) {
	T.Parallel()

	T.Run("ready", func(t *testing.T) {
		t.Parallel()

		client := &mockDBClient{ready: true}
		checker := NewDatabaseChecker("postgres", client)
		ctx := context.Background()

		test.EqOp(t, "postgres", checker.Name())
		err := checker.Check(ctx)
		must.NoError(t, err)
	})

	T.Run("not ready", func(t *testing.T) {
		t.Parallel()

		client := &mockDBClient{ready: false}
		checker := NewDatabaseChecker("postgres", client)
		ctx := context.Background()

		err := checker.Check(ctx)
		must.Error(t, err)
	})

	T.Run("nil client", func(t *testing.T) {
		t.Parallel()

		checker := NewDatabaseChecker("postgres", nil)
		ctx := context.Background()

		err := checker.Check(ctx)
		must.Error(t, err)
		test.StrContains(t, err.Error(), "nil")
	})
}

type mockDBClient struct {
	ready bool
}

func (m *mockDBClient) IsReady(ctx context.Context) bool {
	return m.ready
}

func TestNewCacheChecker(T *testing.T) {
	T.Parallel()

	T.Run("ready", func(t *testing.T) {
		t.Parallel()

		client := &mockCacheClient{err: nil}
		checker := NewCacheChecker("redis", client)
		ctx := context.Background()

		test.EqOp(t, "redis", checker.Name())
		err := checker.Check(ctx)
		must.NoError(t, err)
	})

	T.Run("not ready", func(t *testing.T) {
		t.Parallel()

		client := &mockCacheClient{err: errStub}
		checker := NewCacheChecker("redis", client)
		ctx := context.Background()

		err := checker.Check(ctx)
		must.Error(t, err)
	})

	T.Run("nil client", func(t *testing.T) {
		t.Parallel()

		checker := NewCacheChecker("redis", nil)
		ctx := context.Background()

		err := checker.Check(ctx)
		must.Error(t, err)
		test.StrContains(t, err.Error(), "nil")
	})
}

type mockCacheClient struct {
	err error
}

func (m *mockCacheClient) Ping(ctx context.Context) error {
	return m.err
}

func TestNewMessageQueueChecker(T *testing.T) {
	T.Parallel()

	T.Run("ready", func(t *testing.T) {
		t.Parallel()

		client := &mockMQClient{err: nil}
		checker := NewMessageQueueChecker("redis", client)
		ctx := context.Background()

		test.EqOp(t, "redis", checker.Name())
		err := checker.Check(ctx)
		must.NoError(t, err)
	})

	T.Run("not ready", func(t *testing.T) {
		t.Parallel()

		client := &mockMQClient{err: errStub}
		checker := NewMessageQueueChecker("redis", client)
		ctx := context.Background()

		err := checker.Check(ctx)
		must.Error(t, err)
	})

	T.Run("nil client", func(t *testing.T) {
		t.Parallel()

		checker := NewMessageQueueChecker("redis", nil)
		ctx := context.Background()

		err := checker.Check(ctx)
		must.Error(t, err)
		test.StrContains(t, err.Error(), "nil")
	})
}

type mockMQClient struct {
	err error
}

func (m *mockMQClient) Ping(ctx context.Context) error {
	return m.err
}
