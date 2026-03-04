// Example: OAuth2/OIDC authentication with Google and GitHub.
// Demonstrates first-time registration, returning user login,
// session management, and logout from all devices.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	ducklog "github.com/qwackididuck/duck/log"
	"github.com/qwackididuck/duck/middleware"
	"github.com/qwackididuck/duck/oauth2"
	"github.com/qwackididuck/duck/oauth2/providers"
	"github.com/qwackididuck/duck/oauth2/store"
	"github.com/qwackididuck/duck/server"
)

// =============================================================================
// Application user model and fake database
// =============================================================================

type User struct {
	ID         string
	Email      string
	Name       string
	AvatarURL  string
	Provider   string
	ProviderID string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

type DB struct {
	mu         sync.RWMutex
	byID       map[string]*User
	byProvider map[string]*User
}

func NewDB() *DB {
	return &DB{byID: map[string]*User{}, byProvider: map[string]*User{}}
}

func (d *DB) Upsert(provider, providerID, email, name, avatar string) (*User, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := provider + ":" + providerID

	if u, ok := d.byProvider[key]; ok {
		u.LastSeenAt = time.Now()

		return u, false // returning user
	}

	u := &User{
		ID:         fmt.Sprintf("usr_%d", len(d.byID)+1),
		Email:      email,
		Name:       name,
		AvatarURL:  avatar,
		Provider:   provider,
		ProviderID: providerID,
		CreatedAt:  time.Now(),
		LastSeenAt: time.Now(),
	}

	d.byID[u.ID] = u
	d.byProvider[key] = u

	return u, true // new user
}

func (d *DB) FindByID(id string) *User {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.byID[id]
}

// =============================================================================
// Handlers
// =============================================================================

func handleHome(_ *oauth2.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"message":      "welcome to duck oauth2 example",
			"login_google": "/auth/google/login",
			"login_github": "/auth/github/login",
		}

		// Show extra info if the user is already logged in
		if session, ok := oauth2.SessionFromContext(r.Context()); ok {
			resp["logged_in_as"] = session.UserID
			resp["logout"] = "/auth/logout"
			resp["logout_all"] = "/auth/logout/all (POST)"
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func handleMe(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// session is guaranteed to be present — RequireAuth ensures it
		session, _ := oauth2.SessionFromContext(r.Context())

		user := db.FindByID(session.UserID)
		if user == nil {
			http.Error(w, "user not found", http.StatusNotFound)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         user.ID,
			"email":      user.Email,
			"name":       user.Name,
			"provider":   user.Provider,
			"created_at": user.CreatedAt,
		})
	}
}

// =============================================================================
// Main
// =============================================================================

func main() {
	logger := ducklog.New(
		ducklog.WithFormat(ducklog.FormatJSON),
		ducklog.WithOutput(os.Stdout),
	)

	db := NewDB()

	// Session store — swap for store.NewRedisStore(rdb) in production
	sessionStore := store.NewMemoryStore()

	// Periodically clean up expired sessions
	// (in production this is handled automatically by Redis TTL)

	auth, err := oauth2.New(
		// Providers — credentials from environment variables
		oauth2.WithProvider(providers.Google(
			os.Getenv("GOOGLE_CLIENT_ID"),
			os.Getenv("GOOGLE_CLIENT_SECRET"),
		)),
		oauth2.WithProvider(providers.GitHub(
			os.Getenv("GITHUB_CLIENT_ID"),
			os.Getenv("GITHUB_CLIENT_SECRET"),
		)),

		// Callback URL — must be registered in the provider's developer console
		oauth2.WithRedirectURL("http://localhost:8080/auth/{provider}/callback"),

		// Session store
		oauth2.WithSessionStore(sessionStore),

		// 7-day sessions
		oauth2.WithSessionTTL(7*24*time.Hour),

		// OnLogin is called after every successful OAuth callback.
		// Upsert the user and return the session data duck should store.
		oauth2.WithOnLogin(func(ctx context.Context, id oauth2.Identity) (oauth2.SessionData, error) {
			user, isNew := db.Upsert(
				id.Provider,
				id.ProviderID,
				id.Email,
				id.Name,
				id.AvatarURL,
			)

			logger.InfoContext(ctx, "user authenticated",
				slog.String("user_id", user.ID),
				slog.String("provider", id.Provider),
				slog.Bool("first_login", isNew),
			)

			return oauth2.SessionData{UserID: user.ID}, nil
		}),

		oauth2.WithSuccessRedirect("/me"),
		oauth2.WithLogoutRedirect("/"),
	)
	if err != nil {
		logger.Error("oauth2 setup", "err", err)
		os.Exit(1)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logging(logger))

	// OAuth2 routes — mounted automatically:
	//   GET  /auth/{provider}/login    → redirect to provider
	//   GET  /auth/{provider}/callback → handle redirect back, create session
	//   GET  /auth/logout              → destroy current session
	//   POST /auth/logout/all          → destroy all sessions for the current user
	r.Mount("/auth", auth.Routes())

	// Public route — session loaded if present but not required
	r.With(auth.LoadSession()).Get("/", handleHome(auth))

	// Protected routes — redirects to login if no session
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth())
		r.Get("/me", handleMe(db))
	})

	srv, err := server.NewServer(
		server.WithAddr(":8080"),
		server.WithHandler(r),
		server.WithLogger(logger),
		server.WithShutdownTimeout(10*time.Second),
		server.WithHealthChecks("oauth2-example"),
	)
	if err != nil {
		logger.Error("server setup", "err", err)
		os.Exit(1)
	}

	// Clean up expired sessions in the background
	srv.Go(func(ctx context.Context) {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sessionStore.GC()
			}
		}
	})

	logger.Info("starting",
		slog.String("addr", ":8080"),
		slog.String("google_login", "http://localhost:8080/auth/google/login"),
		slog.String("github_login", "http://localhost:8080/auth/github/login"),
	)

	if err := srv.Start(); err != nil {
		logger.Error("server", "err", err)
		os.Exit(1)
	}
}
