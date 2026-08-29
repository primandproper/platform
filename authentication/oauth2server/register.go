package oauth2server

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MaxRegistrationBodyBytes bounds the registration request body.
//
// /register is the one endpoint here an anonymous caller can write rows
// through, so the body it sends is read with a ceiling rather than to EOF.
// 64 KiB is far more than any legitimate registration — sixteen redirect URIs
// at two kilobytes each does not reach it — and small enough that a caller
// cannot make this server hold a megabyte per open connection.
const MaxRegistrationBodyBytes = 64 << 10

// RegistrationResponse is the RFC 7591 §3.2.1 client information response.
//
// ClientSecret appears exactly once in the lifetime of a registration: here.
// The store holds a digest, so this response is the only time the value exists
// outside the client — which is why the endpoint answers 201 with it rather
// than answering 201 and offering a way to read it back.
type RegistrationResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	ClientName   string `json:"client_name,omitempty"`

	Scope                   string `json:"scope,omitempty"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`

	RedirectURIs  []string `json:"redirect_uris"`
	GrantTypes    []string `json:"grant_types"`
	ResponseTypes []string `json:"response_types"`

	// ClientIDIssuedAt and ClientSecretExpiresAt are seconds since the epoch,
	// as RFC 7591 specifies. Zero for ClientSecretExpiresAt means the secret
	// does not expire.
	ClientIDIssuedAt      int64 `json:"client_id_issued_at"`
	ClientSecretExpiresAt int64 `json:"client_secret_expires_at"`
}

// RegisterHandler serves POST /register: RFC 7591 dynamic client registration.
//
// It is unauthenticated, and has to be — a client that discovered this server
// at runtime holds no credential to authenticate with, and the discovery flow
// exists precisely for clients that were never pre-registered. That makes
// everything an authenticated endpoint would get for free into this server's
// problem: the body is bounded, the metadata is vetted by a RegistrationPolicy,
// and every registration carries an expiry so the table it writes to does not
// grow without limit.
//
// What it cannot decide from in here is how fast an anonymous caller may ask,
// because that rests on who the caller is and this package cannot tell:
// WithRegistrationLimiter is the seam a deployment answers that with, and a
// server built with one returns the handler already behind it — so Handler,
// Mount, and a deployment routing this by hand are all bounded without having
// to arrange it themselves.
//
// A server built with WithDynamicRegistration(false) answers 404 here instead
// of registering anything, so that a deployment which mounted this handler by
// hand cannot end up serving the endpoint its discovery document says it does
// not have. The gate, when there is one, still runs first: a caller hammering
// an endpoint that 404s is the case a bound is for, and spending the check to
// find out it was turned off would be paying at the wrong end.
func (s *Server) RegisterHandler() http.Handler {
	handler := s.registerHandler()

	if s.registrationGate == nil {
		return handler
	}

	return s.registrationGate(handler)
}

// registerHandler is /register itself, without whatever gate wraps it.
func (s *Server) registerHandler() http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		ctx, op := s.o11y.BeginCustom(req.Context(), operationName(endpointRegister))
		defer s.end(ctx, op, endpointRegister, s.clock.Now())

		s.ops.Attempt(ctx, metric.WithAttributes(attribute.String(endpointKey, endpointRegister)))

		if !s.dynamicRegistration {
			// Counted and recorded rather than silently dropped: a client still
			// asking is a client working from a discovery document older than
			// this deployment, which is worth being able to see.
			writeProtocolError(res, s.fail(ctx, op, endpointRegister,
				newProtocolError(http.StatusNotFound, ErrorCodeInvalidRequest,
					"this authorization server does not serve dynamic client registration", ErrRegistrationNotServed)))

			return
		}

		var request RegistrationRequest

		body := http.MaxBytesReader(res, req.Body, MaxRegistrationBodyBytes)
		if err := json.NewDecoder(body).Decode(&request); err != nil {
			code, description := ErrorCodeInvalidClientMetadata, "malformed registration request"
			if _, ok := stderrors.AsType[*http.MaxBytesError](err); ok {
				description = "registration request is too large"
			}

			writeProtocolError(res, s.fail(ctx, op, endpointRegister,
				newProtocolError(http.StatusBadRequest, code, description, err)))

			return
		}

		if err := s.policy.AllowRegistration(ctx, &request); err != nil {
			writeProtocolError(res, s.fail(ctx, op, endpointRegister, registrationError(err)))

			return
		}

		response, perr := s.register(ctx, &request)
		if perr != nil {
			writeProtocolError(res, s.fail(ctx, op, endpointRegister, perr))

			return
		}

		op.Set(clientIDKey, response.ClientID)
		s.clientsRegistered.Add(ctx, 1)

		writeJSON(res, http.StatusCreated, response)
	})
}

// register mints and stores a client.
func (s *Server) register(ctx context.Context, request *RegistrationRequest) (*RegistrationResponse, *protocolError) {
	clientID, _, err := mintCredential(ctx)
	if err != nil {
		return nil, newProtocolError(http.StatusInternalServerError, ErrorCodeServerError, "could not mint a client identifier", err)
	}

	authMethod := request.TokenEndpointAuthMethod
	if authMethod == "" {
		// RFC 7591 §2 makes client_secret_basic the default. A client that
		// wants to be public has to say so, which is the right way round: a
		// registration that omitted the field and got a public client would
		// have silently opted out of the credential it thought it had.
		authMethod = AuthMethodClientBasic
	}

	var secret string

	client := &Client{
		CreatedAt:               s.now(),
		ID:                      clientID,
		Name:                    request.ClientName,
		RedirectURIs:            slices.Clone(request.RedirectURIs),
		GrantTypes:              narrowGrantTypes(request.GrantTypes),
		ResponseTypes:           []string{ResponseTypeCode},
		Scopes:                  s.narrowScopes(request.Scope),
		TokenEndpointAuthMethod: authMethod,
	}

	if s.registrationTTL > 0 {
		client.ExpiresAt = client.CreatedAt.Add(s.registrationTTL)
	}

	if authMethod != AuthMethodNone {
		var digest string
		if secret, digest, err = mintCredential(ctx); err != nil {
			return nil, newProtocolError(http.StatusInternalServerError, ErrorCodeServerError, "could not mint a client secret", err)
		}

		client.SecretHash = digest
	}

	if err = s.store.CreateClient(ctx, client); err != nil {
		return nil, newProtocolError(http.StatusInternalServerError, ErrorCodeServerError, "could not store the registration", err)
	}

	return &RegistrationResponse{
		ClientID:                client.ID,
		ClientSecret:            secret,
		ClientName:              client.Name,
		ClientIDIssuedAt:        client.CreatedAt.Unix(),
		ClientSecretExpiresAt:   secretExpiry(client),
		RedirectURIs:            client.RedirectURIs,
		GrantTypes:              client.GrantTypes,
		ResponseTypes:           client.ResponseTypes,
		Scope:                   joinScopes(client.Scopes),
		TokenEndpointAuthMethod: client.TokenEndpointAuthMethod,
	}, nil
}

// secretExpiry renders RFC 7591's client_secret_expires_at: seconds since the
// epoch, or 0 for a secret that does not expire.
//
// It reports the registration's own expiry, because that is when the secret
// stops working — the whole record lapses together, and telling a client its
// secret is eternal while the registration behind it is not would be a lie the
// client would only discover as a 401.
func secretExpiry(client *Client) int64 {
	if client.SecretHash == "" || client.ExpiresAt.IsZero() {
		return 0
	}

	return client.ExpiresAt.Unix()
}

// narrowGrantTypes reduces a requested grant type list to what this server
// implements.
//
// A registration asking for something unsupported is narrowed rather than
// refused, as RFC 7591 §2 permits, and the response says what was granted — so
// a client asking for the implicit grant is told it did not get it, at
// registration, rather than at the first authorization request.
//
// A registration naming none gets both, which is RFC 7591's own default.
func narrowGrantTypes(requested []string) []string {
	if len(requested) == 0 {
		return []string{GrantTypeAuthorizationCode, GrantTypeRefreshToken}
	}

	var granted []string

	for _, want := range []string{GrantTypeAuthorizationCode, GrantTypeRefreshToken} {
		if slices.Contains(requested, want) {
			granted = append(granted, want)
		}
	}

	if len(granted) == 0 {
		// Nothing recognizable was asked for. The authorization code grant is
		// the only one that can start a flow, so a registration with none of
		// them would be unusable.
		return []string{GrantTypeAuthorizationCode}
	}

	return granted
}

// narrowScopes reduces a requested scope string to what this server issues.
//
// Narrowed rather than refused, and unlike the authorization request that is
// the right answer here: a registration is a client saying what it might ask
// for, and the response tells it what it may. The refusal happens later, at
// /authorize, where a specific token is being minted and silently narrowing one
// would hand back something other than what was asked for.
func (s *Server) narrowScopes(scope string) []string {
	requested := splitScopes(scope)

	if len(s.scopes) == 0 {
		return requested
	}

	var granted []string

	for _, want := range requested {
		if slices.Contains(s.scopes, want) {
			granted = append(granted, want)
		}
	}

	return granted
}

// registrationError renders a policy refusal.
//
// A refusal's own message is sent to the client, which is the opposite of the
// login form's rule and for the opposite reason: there is no human here to
// enumerate anything about, and a client author debugging a rejected
// registration has nothing else to go on.
func registrationError(err error) *protocolError {
	if !stderrors.Is(err, ErrRegistrationRejected) {
		return newProtocolError(http.StatusInternalServerError, ErrorCodeServerError,
			"could not evaluate the registration", err)
	}

	code := ErrorCodeInvalidClientMetadata
	if stderrors.Is(err, ErrRedirectURINotAbsolute) ||
		stderrors.Is(err, ErrRedirectURIInsecure) ||
		stderrors.Is(err, ErrRedirectURIHasFragment) ||
		stderrors.Is(err, ErrRedirectURITooLong) ||
		stderrors.Is(err, ErrNoRedirectURI) ||
		stderrors.Is(err, ErrTooManyRedirectURIs) {
		code = ErrorCodeInvalidRedirectURI
	}

	return newProtocolError(http.StatusBadRequest, code, err.Error(), err)
}
