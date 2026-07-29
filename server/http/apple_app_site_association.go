package http

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"github.com/primandproper/platform-go/v8/encoding"
	"github.com/primandproper/platform-go/v8/observability/logging"
	"github.com/primandproper/platform-go/v8/observability/tracing"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// AppleAppSiteAssociationPath is the well-known path iOS fetches to discover a
// domain's Universal Link configuration. Apple requires it be served over HTTPS
// with no redirects and a JSON content type.
//
// See https://developer.apple.com/documentation/xcode/supporting-associated-domains.
const AppleAppSiteAssociationPath = "/.well-known/apple-app-site-association"

// allPaths is the component pattern that matches every path on the domain, used when a
// config does not narrow Universal Links to specific paths.
const allPaths = "*"

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
	// when TeamID and BundleID are both empty the file is not served at all, so services
	// without an iOS app are unaffected. When either is set, both are required.
	AppleAppSiteAssociationConfig struct {
		_ struct{} `json:"-" yaml:"-"`

		// TeamID is the Apple Developer Team ID (e.g. "ABCD1234XY").
		TeamID string `env:"TEAM_ID" json:"teamID,omitempty" yaml:"teamID,omitempty"`
		// BundleID is the iOS app bundle identifier (e.g. "com.example.ios").
		BundleID string `env:"BUNDLE_ID" json:"bundleID,omitempty" yaml:"bundleID,omitempty"`
		// Paths restricts which URL paths open the app, as Apple component patterns
		// (e.g. "/invitations/*"). Empty grants every path on the domain, which is what a
		// service with no opinion wants; set it when only part of the site should deep-link
		// into the app.
		Paths []string `env:"PATHS" json:"paths,omitempty" yaml:"paths,omitempty"`
		// WebCredentials adds the webcredentials service to the document, which is what
		// lets iOS offer Password AutoFill and shared credentials for this domain. It is
		// off by default: a domain claims it only when the app's entitlements list a
		// matching "webcredentials:" associated domain.
		WebCredentials bool `env:"WEB_CREDENTIALS" json:"webCredentials,omitempty" yaml:"webCredentials,omitempty"`
	}

	// appleAppSiteAssociation is the document served at AppleAppSiteAssociationPath.
	appleAppSiteAssociation struct {
		// WebCredentials is a pointer so an unset one is omitted entirely rather than
		// serialized as an empty object, which iOS would read as a claim with no apps.
		WebCredentials *appleWebCredentials `json:"webcredentials,omitempty"`
		AppLinks       appleAppLinks        `json:"applinks"`
	}

	appleWebCredentials struct {
		Apps []string `json:"apps"`
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
//
// Setting any field counts as intent to serve the document, including Paths or
// WebCredentials alone. That matters because those two are inert without the identifiers:
// a config that scopes paths but whose TeamID never made it out of the environment would
// otherwise validate clean and then quietly serve nothing.
func (cfg *AppleAppSiteAssociationConfig) ValidateWithContext(ctx context.Context) error {
	if cfg == nil || (cfg.TeamID == "" && cfg.BundleID == "" && len(cfg.Paths) == 0 && !cfg.WebCredentials) {
		return nil
	}

	return validation.ValidateStructWithContext(
		ctx,
		cfg,
		validation.Field(&cfg.TeamID, validation.Required, validation.Match(appleTeamIDPattern).Error("must be ten alphanumeric characters")),
		validation.Field(&cfg.BundleID, validation.Required, validation.Match(appleBundleIDPattern).Error("must be a bundle identifier")),
		validation.Field(&cfg.Paths, validation.Each(validation.Required.Error("must not be blank"))),
	)
}

// appID returns the fully qualified application identifier Apple expects, which is
// the team ID and the bundle ID joined by a period.
func (cfg *AppleAppSiteAssociationConfig) appID() string {
	return fmt.Sprintf("%s.%s", cfg.TeamID, cfg.BundleID)
}

// components renders Paths as the component patterns Apple matches incoming URLs
// against. An empty Paths grants every path on the domain.
func (cfg *AppleAppSiteAssociationConfig) components() []appleAppLinkComponent {
	if len(cfg.Paths) == 0 {
		return []appleAppLinkComponent{{Path: allPaths}}
	}

	components := make([]appleAppLinkComponent, 0, len(cfg.Paths))
	for _, path := range cfg.Paths {
		components = append(components, appleAppLinkComponent{Path: path})
	}

	return components
}

// document builds the apple-app-site-association document for the config: the applinks
// service always, scoped to Paths, plus webcredentials when the config claims it.
func (cfg *AppleAppSiteAssociationConfig) document() appleAppSiteAssociation {
	document := appleAppSiteAssociation{
		AppLinks: appleAppLinks{
			Details: []appleAppLinkDetail{
				{
					AppIDs:     []string{cfg.appID()},
					Components: cfg.components(),
				},
			},
		},
	}

	if cfg.WebCredentials {
		document.WebCredentials = &appleWebCredentials{Apps: []string{cfg.appID()}}
	}

	return document
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
		encoding.ContentTypeJSON,
		encoding.WithLogger(logger),
		encoding.WithTracerProvider(tracerProvider),
	)
	document := cfg.document()

	return func(res http.ResponseWriter, req *http.Request) {
		enc.EncodeResponseWithStatus(req.Context(), res, document, http.StatusOK)
	}
}
