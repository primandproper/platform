package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/primandproper/platform-go/v7/encoding"
	loggingnoop "github.com/primandproper/platform-go/v7/observability/logging/noop"
	tracingnoop "github.com/primandproper/platform-go/v7/observability/tracing/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestAppleAppSiteAssociationConfig_Enabled(T *testing.T) {
	T.Parallel()

	T.Run("enabled when both fields are set", func(t *testing.T) {
		t.Parallel()

		cfg := &AppleAppSiteAssociationConfig{TeamID: "ABCD1234XY", BundleID: "com.example.ios"}

		test.True(t, cfg.Enabled())
	})

	T.Run("disabled when empty", func(t *testing.T) {
		t.Parallel()

		cfg := &AppleAppSiteAssociationConfig{}

		test.False(t, cfg.Enabled())
	})

	T.Run("disabled when only team ID is set", func(t *testing.T) {
		t.Parallel()

		cfg := &AppleAppSiteAssociationConfig{TeamID: "ABCD1234XY"}

		test.False(t, cfg.Enabled())
	})

	T.Run("disabled when only bundle ID is set", func(t *testing.T) {
		t.Parallel()

		cfg := &AppleAppSiteAssociationConfig{BundleID: "com.example.ios"}

		test.False(t, cfg.Enabled())
	})

	T.Run("disabled when team ID is malformed", func(t *testing.T) {
		t.Parallel()

		cfg := &AppleAppSiteAssociationConfig{TeamID: "ABCD1234XY.com.example.ios", BundleID: "com.example.ios"}

		test.False(t, cfg.Enabled())
	})

	T.Run("disabled when bundle ID is malformed", func(t *testing.T) {
		t.Parallel()

		cfg := &AppleAppSiteAssociationConfig{TeamID: "ABCD1234XY", BundleID: `com."example".ios`}

		test.False(t, cfg.Enabled())
	})

	T.Run("disabled when nil", func(t *testing.T) {
		t.Parallel()

		var cfg *AppleAppSiteAssociationConfig

		test.False(t, cfg.Enabled())
	})
}

func TestAppleAppSiteAssociationConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		cfg := &AppleAppSiteAssociationConfig{TeamID: "ABCD1234XY", BundleID: "com.example.ios"}

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("empty config is valid", func(t *testing.T) {
		t.Parallel()

		cfg := &AppleAppSiteAssociationConfig{}

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("nil config is valid", func(t *testing.T) {
		t.Parallel()

		var cfg *AppleAppSiteAssociationConfig

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("returns error with missing bundle ID", func(t *testing.T) {
		t.Parallel()

		cfg := &AppleAppSiteAssociationConfig{TeamID: "ABCD1234XY"}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("returns error with missing team ID", func(t *testing.T) {
		t.Parallel()

		cfg := &AppleAppSiteAssociationConfig{BundleID: "com.example.ios"}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("returns error with malformed team ID", func(t *testing.T) {
		t.Parallel()

		cfg := &AppleAppSiteAssociationConfig{TeamID: "ABCD1234XY.com.example.ios", BundleID: "com.example.ios"}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("returns error with malformed bundle ID", func(t *testing.T) {
		t.Parallel()

		cfg := &AppleAppSiteAssociationConfig{TeamID: "ABCD1234XY", BundleID: "com example ios"}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestAppleAppSiteAssociationHandler(T *testing.T) {
	T.Parallel()

	T.Run("serves the association document", func(t *testing.T) {
		t.Parallel()

		handler := AppleAppSiteAssociationHandler(
			&AppleAppSiteAssociationConfig{TeamID: "ABCD1234XY", BundleID: "com.example.ios"},
			loggingnoop.NewLogger(),
			tracingnoop.NewTracerProvider(),
		)

		req := httptest.NewRequest(http.MethodGet, AppleAppSiteAssociationPath, http.NoBody)
		res := httptest.NewRecorder()

		handler.ServeHTTP(res, req)

		test.EqOp(t, http.StatusOK, res.Code)
		test.StrContains(t, res.Header().Get("Content-Type"), "application/json")

		var doc appleAppSiteAssociation
		must.NoError(t, encoding.DecodeJSON(res.Body.Bytes(), &doc))

		must.SliceLen(t, 1, doc.AppLinks.Details)
		test.Eq(t, []string{"ABCD1234XY.com.example.ios"}, doc.AppLinks.Details[0].AppIDs)
		test.Eq(t, []appleAppLinkComponent{{Path: "*"}}, doc.AppLinks.Details[0].Components)
	})

	T.Run("renders the exact shape Apple expects", func(t *testing.T) {
		t.Parallel()

		handler := AppleAppSiteAssociationHandler(
			&AppleAppSiteAssociationConfig{TeamID: "ABCD1234XY", BundleID: "com.example.ios"},
			loggingnoop.NewLogger(),
			tracingnoop.NewTracerProvider(),
		)

		req := httptest.NewRequest(http.MethodGet, AppleAppSiteAssociationPath, http.NoBody)
		res := httptest.NewRecorder()

		handler.ServeHTTP(res, req)

		test.EqOp(
			t,
			`{"applinks":{"details":[{"appIDs":["ABCD1234XY.com.example.ios"],"components":[{"/":"*"}]}]}}`+"\n",
			res.Body.String(),
		)
	})

	T.Run("tolerates a nil logger and tracer provider", func(t *testing.T) {
		t.Parallel()

		handler := AppleAppSiteAssociationHandler(
			&AppleAppSiteAssociationConfig{TeamID: "ABCD1234XY", BundleID: "com.example.ios"},
			nil,
			nil,
		)

		req := httptest.NewRequest(http.MethodGet, AppleAppSiteAssociationPath, http.NoBody)
		res := httptest.NewRecorder()

		handler.ServeHTTP(res, req)

		test.EqOp(t, http.StatusOK, res.Code)
	})

	T.Run("returns 404 when disabled", func(t *testing.T) {
		t.Parallel()

		handler := AppleAppSiteAssociationHandler(&AppleAppSiteAssociationConfig{}, nil, nil)

		req := httptest.NewRequest(http.MethodGet, AppleAppSiteAssociationPath, http.NoBody)
		res := httptest.NewRecorder()

		handler.ServeHTTP(res, req)

		test.EqOp(t, http.StatusNotFound, res.Code)
	})

	T.Run("returns 404 when malformed", func(t *testing.T) {
		t.Parallel()

		handler := AppleAppSiteAssociationHandler(
			&AppleAppSiteAssociationConfig{TeamID: "too-short", BundleID: "com.example.ios"},
			nil,
			nil,
		)

		req := httptest.NewRequest(http.MethodGet, AppleAppSiteAssociationPath, http.NoBody)
		res := httptest.NewRecorder()

		handler.ServeHTTP(res, req)

		test.EqOp(t, http.StatusNotFound, res.Code)
	})

	T.Run("returns 404 when nil", func(t *testing.T) {
		t.Parallel()

		handler := AppleAppSiteAssociationHandler(nil, nil, nil)

		req := httptest.NewRequest(http.MethodGet, AppleAppSiteAssociationPath, http.NoBody)
		res := httptest.NewRecorder()

		handler.ServeHTTP(res, req)

		test.EqOp(t, http.StatusNotFound, res.Code)
	})
}

func TestNewHTTPServer_appleAppSiteAssociation(T *testing.T) {
	T.Parallel()

	T.Run("serves the file when configured", func(t *testing.T) {
		t.Parallel()

		router := testRouter(t)

		_, err := NewHTTPServer(
			&Config{
				Port: 8080,
				AppleAppSiteAssociation: &AppleAppSiteAssociationConfig{
					TeamID:   "ABCD1234XY",
					BundleID: "com.example.ios",
				},
			},
			nil,
			router,
			nil,
			t.Name(),
		)
		must.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, AppleAppSiteAssociationPath, http.NoBody)
		res := httptest.NewRecorder()

		router.Handler().ServeHTTP(res, req)

		test.EqOp(t, http.StatusOK, res.Code)
		test.StrContains(t, res.Body.String(), "ABCD1234XY.com.example.ios")
	})

	T.Run("does not serve the file when unconfigured", func(t *testing.T) {
		t.Parallel()

		router := testRouter(t)

		_, err := NewHTTPServer(&Config{Port: 8080}, nil, router, nil, t.Name())
		must.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, AppleAppSiteAssociationPath, http.NoBody)
		res := httptest.NewRecorder()

		router.Handler().ServeHTTP(res, req)

		test.EqOp(t, http.StatusNotFound, res.Code)
	})

	T.Run("does not serve the file when partially configured", func(t *testing.T) {
		t.Parallel()

		router := testRouter(t)

		_, err := NewHTTPServer(
			&Config{
				Port:                    8080,
				AppleAppSiteAssociation: &AppleAppSiteAssociationConfig{TeamID: "ABCD1234XY"},
			},
			nil,
			router,
			nil,
			t.Name(),
		)
		must.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, AppleAppSiteAssociationPath, http.NoBody)
		res := httptest.NewRecorder()

		router.Handler().ServeHTTP(res, req)

		test.EqOp(t, http.StatusNotFound, res.Code)
	})
}
