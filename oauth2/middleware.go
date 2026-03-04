package oauth2

import (
	"net/http"
)

// RequireAuth returns a middleware that enforces authentication.
// If no valid session is found, the user is redirected to the login page
// of the first registered provider.
//
// Use [LoadSession] for routes that are optionally authenticated.
func (m *Manager) RequireAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := m.sessionFromRequest(r)
			if session == nil {
				// Redirect to the first registered provider login.
				for name := range m.providers {
					http.Redirect(w, r, "/auth/"+name+"/login", http.StatusSeeOther)

					return
				}

				http.Error(w, "not authenticated", http.StatusUnauthorized)

				return
			}

			next.ServeHTTP(w, r.WithContext(contextWithSession(r.Context(), session)))
		})
	}
}

// LoadSession returns a middleware that loads the session if present.
// Unlike [RequireAuth], it does not redirect — the handler is always called.
// Use [SessionFromContext] to check whether a session was found.
//
// Use this for routes that support both authenticated and anonymous access.
func (m *Manager) LoadSession() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := m.sessionFromRequest(r)
			if session != nil {
				r = r.WithContext(contextWithSession(r.Context(), session))
			}

			next.ServeHTTP(w, r)
		})
	}
}
