package oauth2

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	goauth2 "golang.org/x/oauth2"
)

// Routes returns an http.Handler that mounts all auth routes.
// Mount it at a prefix like /auth:
//
//	router.Mount("/auth", auth.Routes())
//
// This exposes:
//
//	GET /auth/{provider}/login      → initiates the OAuth flow
//	GET /auth/{provider}/callback   → handles the provider redirect
//	GET /auth/logout                → destroys the current session
//	POST /auth/logout/all           → destroys all sessions for the current user
func (m *Manager) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/{provider}/login", m.handleLogin)
	r.Get("/{provider}/callback", m.handleCallback)
	r.Get("/logout", m.handleLogout)
	r.Post("/logout/all", m.handleLogoutAll)

	return r
}

// handleLogin initiates the Authorization Code + PKCE flow.
func (m *Manager) handleLogin(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "provider")

	p, ok := m.provider(w, providerName)
	if !ok {
		return
	}

	state, err := generateRandom(stateLen)
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)

		return
	}

	codeVerifier, err := generateRandom(codeVerifierLen)
	if err != nil {
		http.Error(w, "failed to generate code verifier", http.StatusInternalServerError)

		return
	}

	m.setStateCookie(w, statePayload{
		State:        state,
		CodeVerifier: codeVerifier,
	})

	cfg := m.oauth2Config(p)

	// S256 code challenge for PKCE.
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	url := cfg.AuthCodeURL(state,
		goauth2.AccessTypeOffline,
		goauth2.SetAuthURLParam("code_challenge", codeChallenge),
		goauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)

	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// handleCallback handles the provider redirect after user consent.
func (m *Manager) handleCallback(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "provider")

	p, ok := m.provider(w, providerName)
	if !ok {
		return
	}

	// Verify state — CSRF protection.
	returnedState := r.URL.Query().Get("state")

	payload, ok := m.stateFromRequest(r, returnedState)
	if !ok {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)

		return
	}

	m.clearStateCookie(w)

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)

		return
	}

	// Exchange code for tokens with PKCE verifier.
	cfg := m.oauth2Config(p)
	ctx := context.WithValue(r.Context(), goauth2.HTTPClient, m.opts.getHTTPClient())

	token, err := cfg.Exchange(ctx, code,
		goauth2.SetAuthURLParam("code_verifier", payload.CodeVerifier),
	)
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusBadGateway)

		return
	}

	// Extract identity from the token.
	identity, err := p.Identity(r.Context(), token)
	if err != nil {
		http.Error(w, "failed to get user identity", http.StatusBadGateway)

		return
	}

	identity.Provider = providerName

	// Call the application hook.
	sessionData, err := m.opts.onLogin(r.Context(), identity)
	if err != nil {
		http.Error(w, "login rejected", http.StatusForbidden)

		return
	}

	// Create and persist the session.
	sessionID, err := generateRandom(stateLen)
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)

		return
	}

	session := &Session{
		ID:        sessionID,
		UserID:    sessionData.UserID,
		ExpiresAt: time.Now().Add(m.opts.sessionTTLOrDefault()),
		CreatedAt: time.Now(),
	}

	if err := m.opts.store.Save(r.Context(), session); err != nil {
		http.Error(w, "failed to save session", http.StatusInternalServerError)

		return
	}

	m.setCookie(w, sessionID)
	http.Redirect(w, r, m.opts.successRedirect, http.StatusSeeOther)
}

// handleLogout destroys the current session.
func (m *Manager) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(m.opts.sessionCookieName)
	if err == nil {
		_ = m.opts.store.Delete(r.Context(), cookie.Value)
	}

	m.clearCookie(w)
	http.Redirect(w, r, m.opts.logoutRedirect, http.StatusSeeOther)
}

// handleLogoutAll destroys all sessions for the current user.
// Requires an active session — returns 401 if not authenticated.
func (m *Manager) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	session := m.sessionFromRequest(r)
	if session == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)

		return
	}

	if err := m.opts.store.DeleteAllForUser(r.Context(), session.UserID); err != nil {
		http.Error(w, "failed to logout all sessions", http.StatusInternalServerError)

		return
	}

	m.clearCookie(w)
	http.Redirect(w, r, m.opts.logoutRedirect, http.StatusSeeOther)
}

// LogoutAll destroys all sessions for a user programmatically.
// Use this for account deletion, password changes, or impersonation revocation.
func (m *Manager) LogoutAll(ctx context.Context, userID string) error {
	if err := m.opts.store.DeleteAllForUser(ctx, userID); err != nil {
		return fmt.Errorf("duck/oauth2: %w", err)
	}

	return nil
}
