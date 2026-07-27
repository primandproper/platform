package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/primandproper/platform-go/v7/observability/logging"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// AppleAppSiteAssociationPath is the well-known path iOS fetches to discover a
// domain's Universal Link configuration. Apple requires it be served over HTTPS
// with no redirects and a JSON content type.
//
// See https://developer.apple.com/documentation/xcode/supporting-associated-domains.
const AppleAppSiteAssociationPath = "/.well-known/apple-app-site-association"

var (
	// appleTeamIDPattern matches an Apple Developer Team ID (App ID Prefix), which is
	// ten alphanumeric characters.
	appleTeamIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{10}$`)
	// appleBundleIDPattern matches an iOS bundle identifier, which Apple restricts to
	// alphanumerics, hyphens, and periods.
	appleBundleIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)
)

type (
	// AppleAppSiteAssociationConfig holds the configuration for the
	// apple-app-site-association file iOS uses for Universal Links. It is optional:
	// when both fields are empty the file is not served at all, so services without
	// an iOS app are unaffected. When either field is set, both are required.
	AppleAppSiteAssociationConfig struct {
		_ struct{} `json:"-" yaml:"-"`

		// TeamID is the Apple Developer Team ID (e.g. "ABCD1234XY").
		TeamID string `env:"TEAM_ID" json:"teamID,omitempty" yaml:"teamID,omitempty"`
		// BundleID is the iOS app bundle identifier (e.g. "com.example.ios").
		BundleID string `env:"BUNDLE_ID" json:"bundleID,omitempty" yaml:"bundleID,omitempty"`
	}

	// appleAppSiteAssociation is the document served at AppleAppSiteAssociationPath.
	appleAppSiteAssociation struct {
		AppLinks appleAppLinks `json:"applinks"`
	}

	appleAppLinks struct {
		Details []appleAppLinkDetail `json:"details"`
	}

	appleAppLinkDetail struct {
		AppIDs     []string                `json:"appIDs"`
		Components []appleAppLinkComponent `json:"components"`
	}

	appleAppLinkComponent struct {
		Path string `json:"/"`
	}
)

var _ validation.ValidatableWithContext = (*AppleAppSiteAssociationConfig)(nil)

// Enabled indicates whether the apple-app-site-association file should be served.
func (cfg *AppleAppSiteAssociationConfig) Enabled() bool {
	return cfg != nil && cfg.TeamID != "" && cfg.BundleID != ""
}

// ValidateWithContext validates an AppleAppSiteAssociationConfig struct. An entirely
// empty config is valid (the feature is simply off); a partially filled one is not.
func (cfg *AppleAppSiteAssociationConfig) ValidateWithContext(ctx context.Context) error {
	if cfg == nil || (cfg.TeamID == "" && cfg.BundleID == "") {
		return nil
	}

	return validation.ValidateStructWithContext(
		ctx,
		cfg,
		validation.Field(&cfg.TeamID, validation.Required, validation.Match(appleTeamIDPattern).Error("must be ten alphanumeric characters")),
		validation.Field(&cfg.BundleID, validation.Required, validation.Match(appleBundleIDPattern).Error("must be a bundle identifier")),
	)
}

// appID returns the fully qualified application identifier Apple expects, which is
// the team ID and the bundle ID joined by a period.
func (cfg *AppleAppSiteAssociationConfig) appID() string {
	return fmt.Sprintf("%s.%s", cfg.TeamID, cfg.BundleID)
}

// document builds the apple-app-site-association document for the config, granting
// the app every path on the domain.
func (cfg *AppleAppSiteAssociationConfig) document() appleAppSiteAssociation {
	return appleAppSiteAssociation{
		AppLinks: appleAppLinks{
			Details: []appleAppLinkDetail{
				{
					AppIDs:     []string{cfg.appID()},
					Components: []appleAppLinkComponent{{Path: "*"}},
				},
			},
		},
	}
}

// AppleAppSiteAssociationHandler returns an http.HandlerFunc that serves the
// apple-app-site-association document described by cfg as JSON. Register it at
// AppleAppSiteAssociationPath; NewHTTPServer does so automatically when
// Config.AppleAppSiteAssociation is enabled, so this is only needed to serve the
// document from somewhere else (a different mux, a CDN origin, etc).
//
// A disabled config yields a handler that responds 404, so callers never have to
// branch on whether the feature is configured.
//
// The document is rendered once at construction, so the handler only writes bytes.
func AppleAppSiteAssociationHandler(cfg *AppleAppSiteAssociationConfig, logger logging.Logger) http.HandlerFunc {
	if !cfg.Enabled() {
		return http.NotFound
	}

	logger = logging.EnsureLogger(logger)

	body, err := json.Marshal(cfg.document())
	if err != nil {
		logger.Error("rendering apple-app-site-association", err)

		return func(res http.ResponseWriter, _ *http.Request) {
			http.Error(res, "rendering apple-app-site-association", http.StatusInternalServerError)
		}
	}

	return func(res http.ResponseWriter, _ *http.Request) {
		// Apple requires an application/json content type and will not follow redirects.
		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(http.StatusOK)

		if _, writeErr := res.Write(body); writeErr != nil {
			// The status is already written, so the only thing left to do is record it.
			logger.Error("writing apple-app-site-association", writeErr)
		}
	}
}
