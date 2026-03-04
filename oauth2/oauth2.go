// Package oauth2 provides OAuth2/OIDC authentication for duck applications.
//
// It handles the Authorization Code flow with PKCE, session management,
// and provider-specific identity extraction.
//
// Usage:
//
//	auth, err := oauth2.New(
//	    oauth2.WithProvider(providers.Google(clientID, clientSecret)),
//	    oauth2.WithProvider(providers.GitHub(clientID, clientSecret)),
//	    oauth2.WithRedirectURL("http://localhost:8080/auth/{provider}/callback"),
//	    oauth2.WithSessionStore(store.NewMemoryStore()),
//	    oauth2.WithOnLogin(func(ctx context.Context, id oauth2.Identity) (oauth2.SessionData, error) {
//	        user := db.Upsert(id)
//	        return oauth2.SessionData{UserID: user.ID}, nil
//	    }),
//	)
//
//	router.Mount("/auth", auth.Routes())
//	router.With(auth.RequireAuth()).Get("/me", meHandler)
package oauth2

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

// OnLoginFunc is called after every successful OAuth callback.
// It receives the provider identity and must return the session data to store.
// This is where application logic lives: upsert user, check bans, etc.
type OnLoginFunc func(ctx context.Context, identity Identity) (SessionData, error)

// Manager holds the OAuth2 configuration and handles all auth routes.
type Manager struct {
	opts      options
	providers map[string]Provider
}

// New creates a new OAuth2 Manager with the given options.
func New(opts ...Option) (*Manager, error) {
	o := options{
		successRedirect:   "/",
		logoutRedirect:    "/",
		sessionCookieName: "duck_session",
		stateCookieName:   "duck_oauth_state",
	}

	for _, opt := range opts {
		opt(&o)
	}

	if o.store == nil {
		return nil, errors.New("duck/oauth2: session store is required — use WithSessionStore()")
	}

	if o.onLogin == nil {
		return nil, errors.New("duck/oauth2: OnLogin hook is required — use WithOnLogin()")
	}

	if o.redirectURL == "" {
		return nil, errors.New("duck/oauth2: redirect URL is required — use WithRedirectURL()")
	}

	if len(o.providers) == 0 {
		return nil, errors.New("duck/oauth2: at least one provider is required — use WithProvider()")
	}

	m := &Manager{
		opts:      o,
		providers: make(map[string]Provider, len(o.providers)),
	}

	for _, p := range o.providers {
		m.providers[p.Name()] = p
	}

	return m, nil
}

// redirectURLForProvider returns the callback URL with the provider name substituted.
func (m *Manager) redirectURLForProvider(providerName string) string {
	return strings.ReplaceAll(m.opts.redirectURL, "{provider}", providerName)
}

// oauth2Config builds the golang.org/x/oauth2 config for a provider.
func (m *Manager) oauth2Config(p Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.ClientID(),
		ClientSecret: p.ClientSecret(),
		RedirectURL:  m.redirectURLForProvider(p.Name()),
		Scopes:       p.Scopes(),
		Endpoint:     p.Endpoint(),
	}
}

// provider returns the provider by name or an HTTP 400 if unknown.
func (m *Manager) provider(w http.ResponseWriter, providerName string) (Provider, bool) {
	p, ok := m.providers[providerName]
	if !ok {
		http.Error(w, fmt.Sprintf("unknown provider %q", providerName), http.StatusBadRequest)

		return nil, false
	}

	return p, true
}
