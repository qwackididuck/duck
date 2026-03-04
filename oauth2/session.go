package oauth2

import (
	"context"
	"net/http"
	"time"
)

// Session holds the server-side session data associated with a session cookie.
type Session struct {
	// ID is the unique session identifier stored in the cookie.
	ID string `json:"id"`

	// UserID is the application user ID returned by OnLoginFunc.
	UserID string `json:"userId"`

	// ImpersonatedBy is the admin user ID if this is an impersonation session.
	// Empty for normal sessions.
	ImpersonatedBy string `json:"impersonatedBy,omitempty"`

	// ExpiresAt is when this session becomes invalid.
	ExpiresAt time.Time `json:"expiresAt"`

	// CreatedAt is when this session was created.
	CreatedAt time.Time `json:"createdAt"`
}

// IsExpired reports whether the session has expired.
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// IsImpersonated reports whether this is an impersonation session.
func (s *Session) IsImpersonated() bool {
	return s.ImpersonatedBy != ""
}

// SessionData is what OnLoginFunc returns — the minimal data duck needs to
// build a Session. Add fields here if your application needs to store more
// data in the session (e.g. roles, tenant).
type SessionData struct {
	// UserID is the application user ID.
	UserID string
}

// SessionStore is the interface for persisting sessions.
// Implement this to use any backend (Postgres, MongoDB, etc.).
type SessionStore interface {
	// Save persists a session. Overwrites any existing session with the same ID.
	Save(ctx context.Context, session *Session) error

	// Get retrieves a session by ID.
	// Returns ErrSessionNotFound if the session does not exist.
	Get(ctx context.Context, sessionID string) (*Session, error)

	// Delete removes a single session by ID.
	// Must not return an error if the session does not exist.
	Delete(ctx context.Context, sessionID string) error

	// DeleteAllForUser removes all sessions for a given user ID.
	// This is the mechanism for "logout from all devices" and impersonation revocation.
	DeleteAllForUser(ctx context.Context, userID string) error
}

// SessionFromContext returns the session stored in the context by [LoadSession]
// or [RequireAuth]. Returns false if no session is present.
func SessionFromContext(ctx context.Context) (*Session, bool) {
	s, ok := ctx.Value(sessionContextKey).(*Session)

	return s, ok && s != nil
}

// contextWithSession returns a new context carrying the session.
func contextWithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, s)
}

// setCookie writes the session cookie to the response.
func (m *Manager) setCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.opts.sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   int(m.opts.sessionTTLOrDefault().Seconds()),
		HttpOnly: true,
		Secure:   m.opts.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearCookie removes the session cookie.
func (m *Manager) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.opts.sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.opts.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// sessionFromRequest reads and validates the session from the request cookie.
// Returns nil if no cookie is present, the session is not found, or expired.
func (m *Manager) sessionFromRequest(r *http.Request) *Session {
	cookie, err := r.Cookie(m.opts.sessionCookieName)
	if err != nil {
		return nil
	}

	session, err := m.opts.store.Get(r.Context(), cookie.Value)
	if err != nil {
		return nil
	}

	if session.IsExpired() {
		_ = m.opts.store.Delete(r.Context(), session.ID)

		return nil
	}

	return session
}
