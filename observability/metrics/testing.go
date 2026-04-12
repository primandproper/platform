package metrics

import (
	"testing"

	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

func Int64CounterForTest(t *testing.T, name string) metric.Int64Counter {
	t.Helper()

	x, err := otel.Meter("testing").Int64Counter(name)
	must.NoError(t, err)

	return x
}
