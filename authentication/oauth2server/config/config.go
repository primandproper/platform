package oauth2servercfg

import (
	"context"
	"time"

	"github.com/primandproper/platform-go/v14/authentication/oauth2server"
	oauth2memory "github.com/primandproper/platform-go/v14/authentication/oauth2server/memory"
	"github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/internal/cfgnorm"
	"github.com/primandproper/platform-go/v14/pointer"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config assembles an authorization server from environment configuration.
type Config struct {
	_ struct{} `json:"-" yaml:"-"`

	// SweepInterval is how often the store removes records past their
	// deadlines. Unset takes oauth2server.DefaultSweepInterval; zero starts no
	// sweeper.
	//
	// Starting no sweeper is right when a scheduler calls Sweep instead — one
	// sweep for the fleet rather than one per replica — and wrong when nothing
	// else does, since the tables then grow with every login attempt and every
	// anonymous registration. That asymmetry is why the sweeper is what an
	// unconfigured Config gets, and why the pointer is here: unset and zero are
	// different answers, and a time.Duration has only one way to say both.
	//
	// In the environment that is an absent SWEEP_INTERVAL against
	// SWEEP_INTERVAL=0.
	SweepInterval *time.Duration `env:"SWEEP_INTERVAL" json:"sweepInterval,omitempty" yaml:"sweepInterval,omitempty"`

	// ClientRegistrationTTL is how long a dynamically registered client lasts
	// before it must register again. Unset takes
	// oauth2server.DefaultClientRegistrationTTL; zero means registrations never
	// lapse.
	//
	// Never lapsing is a real choice, and one to make deliberately: on a server
	// that serves registration it leaves an unauthenticated endpoint writing
	// rows nothing removes. It is the pointer that makes the choice reachable —
	// unset and zero are different answers, and a time.Duration has only one
	// way to say both. The three lifetimes above stay plain durations because
	// for them a zero genuinely is an absence; this is the one option in the
	// group that reads zero as a value.
	//
	// In the environment that is an absent CLIENT_REGISTRATION_TTL against
	// CLIENT_REGISTRATION_TTL=0. A negative value is refused by validation
	// rather than read as zero.
	ClientRegistrationTTL *time.Duration `env:"CLIENT_REGISTRATION_TTL" json:"clientRegistrationTTL,omitempty" yaml:"clientRegistrationTTL,omitempty"`

	// Issuer is this server's identity: an https URL with no query or
	// fragment, no trailing slash. Every endpoint in the discovery document is
	// derived from it, and a client compares it against the "iss" in an
	// authorization response.
	//
	// http is accepted only for a loopback host, which is what a development
	// server runs on.
	Issuer string `env:"ISSUER" json:"issuer,omitempty" yaml:"issuer,omitempty"`

	// ServiceDocumentation is an optional URL advertised in the discovery
	// document.
	ServiceDocumentation string `env:"SERVICE_DOCUMENTATION" json:"serviceDocumentation,omitempty" yaml:"serviceDocumentation,omitempty"`

	// Scopes are the scopes this server issues. An authorization request for
	// anything outside the set is refused rather than narrowed; leaving it
	// empty accepts whatever a client registered for.
	//
	// Comma-separated in the environment, which is env's default for a slice
	// and so carries no envSeparator tag: spelling the default out is what put
	// the tag formatter and the tag linter in disagreement over where it goes.
	Scopes []string `env:"SCOPES" json:"scopes,omitempty" yaml:"scopes,omitempty"`

	// Resources are the RFC 8707 resource indicators this server mints tokens
	// for, and which become an access token's audience. Leaving it empty
	// accepts whatever a client asks for, which still binds the token — to
	// something the client chose rather than something this server asserted.
	Resources []string `env:"RESOURCES" json:"resources,omitempty" yaml:"resources,omitempty"`

	// AuthorizationCodeTTL is how long an authorization code is redeemable.
	AuthorizationCodeTTL time.Duration `env:"AUTHORIZATION_CODE_TTL" json:"authorizationCodeTTL,omitempty" yaml:"authorizationCodeTTL,omitempty"`

	// AccessTokenTTL is how long an access token is usable.
	//
	// Raising it does not lengthen a session — the refresh token decides that.
	// It lengthens how long a leaked token works.
	AccessTokenTTL time.Duration `env:"ACCESS_TOKEN_TTL" json:"accessTokenTTL,omitempty" yaml:"accessTokenTTL,omitempty"`

	// RefreshTokenTTL is how long a refresh token is exchangeable.
	RefreshTokenTTL time.Duration `env:"REFRESH_TOKEN_TTL" json:"refreshTokenTTL,omitempty" yaml:"refreshTokenTTL,omitempty"`

	// DisableDynamicRegistration stops this server serving RFC 7591 dynamic
	// client registration: /register is not routed, and the discovery document
	// leaves registration_endpoint out rather than naming an endpoint that
	// answers 404.
	//
	// It is spelled as a disable rather than an enable so that an unset
	// environment produces the protocol's own behavior — a client that
	// discovered this server at runtime can register. Turn it off for a
	// deployment whose clients are administered elsewhere, where an anonymous
	// endpoint writing to the same client table is a way around whatever
	// administers them.
	DisableDynamicRegistration bool `env:"DISABLE_DYNAMIC_REGISTRATION" json:"disableDynamicRegistration,omitempty" yaml:"disableDynamicRegistration,omitempty"`

	// DisableRefreshReuseDetection turns off revoking a token family when an
	// already-redeemed refresh token is presented.
	//
	// It is spelled as a disable rather than an enable so that the safe
	// behavior is what an unset environment produces. Turning it off turns
	// rotation into bookkeeping: the replay is refused and the copy the
	// attacker is using keeps working.
	DisableRefreshReuseDetection bool `env:"DISABLE_REFRESH_REUSE_DETECTION" json:"disableRefreshReuseDetection,omitempty" yaml:"disableRefreshReuseDetection,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults fills in zero fields.
//
// The lifetimes are defaulted here as well as in the Server so that a Config
// reads as what it will actually do, rather than as a set of zeroes whose
// meaning is somewhere else. SweepInterval and ClientRegistrationTTL are
// defaulted on the same terms, but need a pointer to do it: only a nil is
// unset, so a zero reaching this method is a deployment asking for no sweeper,
// or for registrations that never lapse, and is left alone.
func (cfg *Config) EnsureDefaults() {
	if cfg.AuthorizationCodeTTL == 0 {
		cfg.AuthorizationCodeTTL = oauth2server.DefaultAuthorizationCodeTTL
	}
	if cfg.AccessTokenTTL == 0 {
		cfg.AccessTokenTTL = oauth2server.DefaultAccessTokenTTL
	}
	if cfg.RefreshTokenTTL == 0 {
		cfg.RefreshTokenTTL = oauth2server.DefaultRefreshTokenTTL
	}
	if cfg.ClientRegistrationTTL == nil {
		cfg.ClientRegistrationTTL = pointer.To(oauth2server.DefaultClientRegistrationTTL)
	}
	cfg.SweepInterval = cfgnorm.EnsureSweepInterval(cfg.SweepInterval, oauth2server.DefaultSweepInterval)
}

// ValidateWithContext validates a Config struct.
//
// The issuer is not validated here beyond being present. Whether it is a legal
// issuer is oauth2server.NewServer's answer, and duplicating the rule would
// create a second place for it to be wrong — NewStore does not need one at all.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.AuthorizationCodeTTL, validation.Min(time.Duration(0))),
		validation.Field(&cfg.AccessTokenTTL, validation.Min(time.Duration(0))),
		validation.Field(&cfg.RefreshTokenTTL, validation.Min(time.Duration(0))),
		// Min reads through the pointer and has nothing to say about a nil or
		// a zero, so the rule refuses exactly the negatives the option would
		// otherwise have folded into zero.
		validation.Field(&cfg.ClientRegistrationTTL, validation.Min(time.Duration(0))),
		validation.Field(&cfg.SweepInterval, cfgnorm.SweepIntervalRule),
	)
}

// NewStore builds the in-memory Store.
//
// A deployment keeping records in SQL calls oauth2dbcfg.NewStore instead, which
// selects between that store and this one.
func NewStore(ctx context.Context, cfg *Config, opts ...Option) (oauth2server.Store, error) {
	if cfg == nil {
		return nil, errors.ErrNilInputParameter
	}

	o := newOptions(opts)

	cfg.EnsureDefaults()

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating oauth2 server config")
	}

	return oauth2memory.NewStore(append([]oauth2memory.Option{
		oauth2memory.WithLogger(o.logger),
		oauth2memory.WithTracerProvider(o.tracerProvider),
		// Bound to the caller's context: the sweep stops when whatever scope
		// owns this store does.
		oauth2memory.WithSweeper(ctx, pointer.Dereference(cfg.SweepInterval)),
	}, o.memoryStore...)...), nil
}

// NewServer builds an authorization server over store.
//
// The store is a parameter rather than something this function builds, and that
// is the seam the tier split runs along — see the package documentation. A
// caller that wants the store selected for it, from a provider string, calls
// oauth2dbcfg.NewServer.
//
// authenticator is a parameter rather than an option because it is the one
// thing no configuration can supply: it is how this deployment identifies a
// human, and a default would be a server that issues authorization codes to
// whoever asks. The login form does have a default, so it is an option —
// WithLoginRenderer.
func NewServer(
	ctx context.Context,
	cfg *Config,
	store oauth2server.Store,
	authenticator oauth2server.SubjectAuthenticator,
	opts ...Option,
) (*oauth2server.Server, error) {
	if cfg == nil {
		return nil, errors.ErrNilInputParameter
	}

	o := newOptions(opts)

	cfg.EnsureDefaults()

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating oauth2 server config")
	}

	return oauth2server.NewServer(cfg.Issuer, store, authenticator, cfg.serverOptions(o)...)
}

// serverOptions renders the Config as server options, ahead of whatever the
// caller passed.
func (cfg *Config) serverOptions(o *options) []oauth2server.Option {
	opts := []oauth2server.Option{
		oauth2server.WithAuthorizationCodeTTL(cfg.AuthorizationCodeTTL),
		oauth2server.WithAccessTokenTTL(cfg.AccessTokenTTL),
		oauth2server.WithRefreshTokenTTL(cfg.RefreshTokenTTL),
		oauth2server.WithClientRegistrationTTL(pointer.Dereference(cfg.ClientRegistrationTTL)),
		oauth2server.WithRefreshReuseDetection(!cfg.DisableRefreshReuseDetection),
		oauth2server.WithDynamicRegistration(!cfg.DisableDynamicRegistration),
		oauth2server.WithServiceDocumentation(cfg.ServiceDocumentation),
		oauth2server.WithScopes(cfg.Scopes...),
		oauth2server.WithResources(cfg.Resources...),
		oauth2server.WithLogger(o.logger),
		oauth2server.WithTracerProvider(o.tracerProvider),
		oauth2server.WithMetricsProvider(o.metricsProvider),
	}

	return append(opts, o.server...)
}
