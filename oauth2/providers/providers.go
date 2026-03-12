// Package providers contains pre-configured OAuth2/OIDC providers for duck.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	goauth2 "golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/qwackididuck/duck/oauth2"
)

// googleProvider implements oauth2.Provider for Google.
type googleProvider struct {
	clientID     string
	clientSecret string
}

// Google returns a Provider configured for Google OAuth2/OIDC.
// Get credentials at https://console.cloud.google.com/apis/credentials
func Google(clientID, clientSecret string) oauth2.Provider {
	return &googleProvider{clientID: clientID, clientSecret: clientSecret}
}

func (g *googleProvider) Name() string         { return "google" }
func (g *googleProvider) ClientID() string     { return g.clientID }
func (g *googleProvider) ClientSecret() string { return g.clientSecret }
func (g *googleProvider) Scopes() []string {
	return []string{"openid", "email", "profile"}
}
func (g *googleProvider) Endpoint() goauth2.Endpoint { return google.Endpoint }

func (g *googleProvider) Identity(ctx context.Context, token *goauth2.Token) (oauth2.Identity, error) {
	// Google returns an id_token — we decode it to get the user info.
	// For simplicity we call the userinfo endpoint instead of decoding the JWT.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v3/userinfo", http.NoBody)
	if err != nil {
		return oauth2.Identity{}, fmt.Errorf("google: create userinfo request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return oauth2.Identity{}, fmt.Errorf("google: userinfo request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return oauth2.Identity{}, fmt.Errorf("google: userinfo returned %d", resp.StatusCode)
	}

	var info struct {
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return oauth2.Identity{}, fmt.Errorf("google: decode userinfo: %w", err)
	}

	return oauth2.Identity{
		ProviderID: info.Sub,
		Email:      info.Email,
		Name:       info.Name,
		AvatarURL:  info.Picture,
		RawToken:   token,
	}, nil
}

// githubProvider implements oauth2.Provider for GitHub.
type githubProvider struct {
	clientID     string
	clientSecret string
}

// GitHub returns a Provider configured for GitHub OAuth2.
// Get credentials at https://github.com/settings/developers
func GitHub(clientID, clientSecret string) oauth2.Provider {
	return &githubProvider{clientID: clientID, clientSecret: clientSecret}
}

func (g *githubProvider) Name() string         { return "github" }
func (g *githubProvider) ClientID() string     { return g.clientID }
func (g *githubProvider) ClientSecret() string { return g.clientSecret }
func (g *githubProvider) Scopes() []string     { return []string{"read:user", "user:email"} }
func (g *githubProvider) Endpoint() goauth2.Endpoint {
	return goauth2.Endpoint{ //nolint:gosec // G101: these are public OAuth2 endpoint URLs, not credentials
		AuthURL:  "https://github.com/login/oauth/authorize",
		TokenURL: "https://github.com/login/oauth/access_token",
	}
}

func (g *githubProvider) Identity(ctx context.Context, token *goauth2.Token) (oauth2.Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", http.NoBody)
	if err != nil {
		return oauth2.Identity{}, fmt.Errorf("github: create user request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return oauth2.Identity{}, fmt.Errorf("github: user request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return oauth2.Identity{}, fmt.Errorf("github: user API returned %d", resp.StatusCode)
	}

	var info struct {
		ID        int    `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatarUrl"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return oauth2.Identity{}, fmt.Errorf("github: decode user: %w", err)
	}

	name := info.Name
	if name == "" {
		name = info.Login
	}

	return oauth2.Identity{
		ProviderID: strconv.Itoa(info.ID),
		Email:      info.Email,
		Name:       name,
		AvatarURL:  info.AvatarURL,
		RawToken:   token,
	}, nil
}
