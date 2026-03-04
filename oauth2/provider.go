package oauth2

import (
	"context"

	"golang.org/x/oauth2"
)

// Provider is the interface that each OAuth2/OIDC provider must implement.
// It supplies the configuration needed to build an oauth2.Config and extract
// user identity from the token response.
//
// Implement this interface to add custom providers (Keycloak, Okta, etc.).
type Provider interface {
	// Name returns the unique provider identifier used in URLs.
	// e.g. "google", "github"
	Name() string

	// ClientID returns the OAuth2 client ID.
	ClientID() string

	// ClientSecret returns the OAuth2 client secret.
	ClientSecret() string

	// Scopes returns the OAuth2 scopes to request.
	Scopes() []string

	// Endpoint returns the provider authorization and token endpoints.
	Endpoint() oauth2.Endpoint

	// Identity extracts the user identity from the token response.
	// It may call the provider userinfo endpoint if needed.
	// ctx is the request context — use it for all outgoing HTTP calls.
	Identity(ctx context.Context, token *oauth2.Token) (Identity, error)
}

// Identity is the normalized user identity returned by any provider.
// Fields may be empty if the provider does not supply them.
type Identity struct {
	// Provider is the provider name e.g. "google", "github".
	Provider string

	// ProviderID is the user unique ID at the provider.
	// Always present.
	ProviderID string

	// Email is the user email address.
	Email string

	// Name is the user display name.
	Name string

	// AvatarURL is the URL of the user profile picture.
	AvatarURL string

	// RawToken is the original OAuth2 token — available if the application
	// needs to make provider API calls on behalf of the user.
	RawToken *oauth2.Token
}
