package http

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"github.com/primandproper/platform-go/v7/encoding"
	"github.com/primandproper/platform-go/v7/observability/logging"
	"github.com/primandproper/platform-go/v7/observability/tracing"

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

// Enabled indicates whether the apple-app-site-association file should be served,
// which requires both identifiers to be present and well-formed. A malformed config
// reports disabled here and an error from ValidateWithContext, so a service that
// skips validation serves nothing rather than a document iOS would reject.
func (cfg *AppleAppSiteAssociationConfig) Enabled() bool {
	return cfg != nil &&
		appleTeamIDPattern.MatchString(cfg.TeamID) &&
		appleBundleIDPattern.MatchString(cfg.BundleID)
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
// apple-app-site-association document described by cfg. Register it at
// AppleAppSiteAssociationPath; NewHTTPServer does so automatically when
// Config.AppleAppSiteAssociation is enabled, so this is only needed to serve the
// document from somewhere else (a different mux, a CDN origin, etc).
//
// A config that is empty or malformed yields a handler that responds 404, so callers
// never have to branch on whether the feature is configured.
func AppleAppSiteAssociationHandler(
	cfg *AppleAppSiteAssociationConfig,
	logger logging.Logger,
	tracerProvider tracing.TracerProvider,
) http.HandlerFunc {
	if !cfg.Enabled() {
		return http.NotFound
	}

	// Apple only accepts JSON here, so this uses its own JSON encoder rather than the
	// service's configured one, which may be YAML, XML, or anything else.
	enc := encoding.NewServerEncoderDecoder(
		logging.EnsureLogger(logger),
		tracing.EnsureTracerProvider(tracerProvider),
		encoding.ContentTypeJSON,
	)
	document := cfg.document()

	return func(res http.ResponseWriter, req *http.Request) {
		enc.EncodeResponseWithStatus(req.Context(), res, document, http.StatusOK)
	}
}
