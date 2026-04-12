package noop

import (
	"net/http"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestPaymentManager_HandleEventWebhook(T *testing.T) {
	T.Parallel()

	T.Run("returns nil", func(t *testing.T) {
		t.Parallel()
		mgr := NewPaymentManager()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.com/webhook", http.NoBody)
		must.NoError(t, err)

		test.NoError(t, mgr.HandleEventWebhook(req))
	})
}

func TestPaymentManager_ImplementsInterface(T *testing.T) {
	T.Parallel()

	T.Run("satisfies PaymentManager", func(t *testing.T) {
		t.Parallel()
		_ = NewPaymentManager()
	})
}
