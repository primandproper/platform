package routing_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/routing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// readinessReport is the body a probe answers with whether or not it is ready:
// one shape, two statuses.
type readinessReport struct {
	Detail string `json:"detail"`
	Ready  bool   `json:"ready"`
}

// readinessRoute registers a probe that answers 503 when not ready.
func readinessRoute(r *routing.Router, ready bool) {
	routing.Get(r, "/ready", func(ctx context.Context, _ routing.Empty) (readinessReport, error) {
		report := readinessReport{Ready: ready, Detail: "cache"}
		if !ready {
			routing.SetResponseStatus(ctx, http.StatusServiceUnavailable)
		}

		return report, nil
	}, routing.WithEnvelope(false),
		routing.WithAdditionalResponse(http.StatusServiceUnavailable, new(readinessReport), "not ready"))
}

func TestRouter_SetResponseStatus(T *testing.T) {
	T.Parallel()

	T.Run("a handler that names no status gets the registered one", func(t *testing.T) {
		t.Parallel()

		r := buildTestRouter(t)
		readinessRoute(r, true)
		must.NoError(t, r.Err())

		rec := doRequest(t, r, http.MethodGet, "/ready", "")

		test.EqOp(t, http.StatusOK, rec.Code)

		var got readinessReport
		must.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		test.True(t, got.Ready)
	})

	T.Run("a handler naming a status answers with it, body unchanged", func(t *testing.T) {
		t.Parallel()

		r, logger, spans := observedRouter(t)
		readinessRoute(r, false)
		must.NoError(t, r.Err())

		rec := doRequest(t, r, http.MethodGet, "/ready", "")

		test.EqOp(t, http.StatusServiceUnavailable, rec.Code)

		var got readinessReport
		must.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		test.False(t, got.Ready)
		test.EqOp(t, "cache", got.Detail)

		// The whole reason this is not a returned error: an unready probe is a
		// successful report, and a poll of it every few seconds must not fill the
		// logs with service faults.
		test.SliceEmpty(t, logger.at(logging.ErrorLevel))
		test.SliceEmpty(t, spans.errored())
	})

	T.Run("it applies to an enveloped response", func(t *testing.T) {
		t.Parallel()

		r := buildTestRouter(t)
		routing.Get(r, "/ready", func(ctx context.Context, _ routing.Empty) (readinessReport, error) {
			routing.SetResponseStatus(ctx, http.StatusServiceUnavailable)

			return readinessReport{Detail: "cache"}, nil
		})
		must.NoError(t, r.Err())

		rec := doRequest(t, r, http.MethodGet, "/ready", "")

		test.EqOp(t, http.StatusServiceUnavailable, rec.Code)

		var got envelope[readinessReport]
		must.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		test.EqOp(t, "cache", got.Data.Detail)
	})

	T.Run("it applies to a bodyless response", func(t *testing.T) {
		t.Parallel()

		r := buildTestRouter(t)
		routing.Delete(r, "/things/{id:uint64}", func(ctx context.Context, _ deleteInput) (routing.Empty, error) {
			routing.SetResponseStatus(ctx, http.StatusAccepted)

			return routing.Empty{}, nil
		}, routing.WithResponseStatus(http.StatusNoContent))
		must.NoError(t, r.Err())

		rec := doRequest(t, r, http.MethodDelete, "/things/5", "")

		test.EqOp(t, http.StatusAccepted, rec.Code)
		test.EqOp(t, 0, rec.Body.Len())
	})

	T.Run("the last status named wins", func(t *testing.T) {
		t.Parallel()

		r := buildTestRouter(t)
		routing.Get(r, "/ready", func(ctx context.Context, _ routing.Empty) (readinessReport, error) {
			test.True(t, routing.SetResponseStatus(ctx, http.StatusServiceUnavailable))
			test.True(t, routing.SetResponseStatus(ctx, http.StatusOK))

			return readinessReport{Ready: true}, nil
		})
		must.NoError(t, r.Err())

		test.EqOp(t, http.StatusOK, doRequest(t, r, http.MethodGet, "/ready", "").Code)
	})

	T.Run("an error is answered with the error's status, not the named one", func(t *testing.T) {
		t.Parallel()

		r := buildTestRouter(t)
		routing.Get(r, "/ready", func(ctx context.Context, _ routing.Empty) (readinessReport, error) {
			routing.SetResponseStatus(ctx, http.StatusServiceUnavailable)

			return readinessReport{}, errors.New("the database is on fire")
		})
		must.NoError(t, r.Err())

		test.EqOp(t, http.StatusInternalServerError, doRequest(t, r, http.MethodGet, "/ready", "").Code)
	})

	T.Run("a status no response can carry is refused", func(t *testing.T) {
		t.Parallel()

		r := buildTestRouter(t)
		routing.Get(r, "/ready", func(ctx context.Context, _ routing.Empty) (readinessReport, error) {
			test.False(t, routing.SetResponseStatus(ctx, 7))
			test.False(t, routing.SetResponseStatus(ctx, 1000))

			return readinessReport{Ready: true}, nil
		})
		must.NoError(t, r.Err())

		// Refused rather than clamped, and the registered status still serves the
		// response: a ResponseWriter panics on a status outside this range.
		test.EqOp(t, http.StatusOK, doRequest(t, r, http.MethodGet, "/ready", "").Code)
	})

	T.Run("outside a routed request it records nothing and says so", func(t *testing.T) {
		t.Parallel()

		test.False(t, routing.SetResponseStatus(t.Context(), http.StatusServiceUnavailable))
	})

	T.Run("the documented statuses are the registered one and any declared", func(t *testing.T) {
		t.Parallel()

		r := buildTestRouter(t)
		readinessRoute(r, true)
		must.NoError(t, r.Err())

		item, ok := r.Spec().Paths.MapOfPathItemValues["/ready"]
		must.True(t, ok)

		responses := item.MapOfOperationValues["get"].Responses.MapOfResponseOrRefValues
		test.MapContainsKey(t, responses, "200")
		test.MapContainsKey(t, responses, "503")
	})
}
