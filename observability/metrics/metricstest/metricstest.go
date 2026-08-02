/*
Package metricstest provides metric instruments for tests.

It is a separate package rather than a file in observability/metrics because
anything importing "testing" from a non-test file drags the testing package —
and, here, shoenig/test — into every production binary that transitively imports
it. Consumers reach for these from their own _test.go files, so the split costs
them nothing.
*/
package metricstest

import (
	"testing"

	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Int64Counter builds a counter from the process-global meter provider, failing
// the test if it cannot.
func Int64Counter(t *testing.T, name string) metric.Int64Counter {
	t.Helper()

	x, err := otel.Meter("testing").Int64Counter(name)
	must.NoError(t, err)

	return x
}
