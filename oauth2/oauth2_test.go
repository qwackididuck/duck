package oauth2_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goauth2 "golang.org/x/oauth2"

	"github.com/qwackididuck/duck/oauth2"
	"github.com/qwackididuck/duck/oauth2/store"
)

// mockProvider implements oauth2.Provider for tests — no real HTTP calls.
type mockProvider struct {
	name     string
	identity oauth2.Identity
}

func (m *mockProvider) Name() string         { return m.name }
func (m *mockProvider) ClientID() string     { return "test-client-id" }
func (m *mockProvider) ClientSecret() string { return "test-client-secret" }
func (m *mockProvider) Scopes() []string     { return []string{"openid"} }
func (m *mockProvider) Endpoint() goauth2.Endpoint {
	return goauth2.Endpoint{AuthURL: "/auth", TokenURL: "/token"}
}
func (m *mockProvider) Identity(_ context.Context, _ *goauth2.Token) (oauth2.Identity, error) {
	return m.identity, nil
}

func newTestManager(t *testing.T) *oauth2.Manager {
	t.Helper()

	memStore := store.NewMemoryStore()

	m, err := oauth2.New(
		oauth2.WithProvider(&mockProvider{
			name: "mock",
			identity: oauth2.Identity{
				ProviderID: "provider-user-123",
				Email:      "alice@example.com",
				Name:       "Alice",
			},
		}),
		oauth2.WithRedirectURL("http://localhost/auth/{provider}/callback"),
		oauth2.WithSessionStore(memStore),
		oauth2.WithOnLogin(func(_ context.Context, id oauth2.Identity) (oauth2.SessionData, error) {
			return oauth2.SessionData{UserID: "app-user-" + id.ProviderID}, nil
		}),
		oauth2.WithSuccessRedirect("/dashboard"),
		oauth2.WithLogoutRedirect("/"),
	)
	if err != nil {
		t.Fatalf("oauth2.New: %v", err)
	}

	return m
}

func TestNew_validationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    []oauth2.Option
		wantErr string
	}{
		{
			name: "missing store",
			opts: []oauth2.Option{oauth2.WithProvider(&mockProvider{name: "mock"}), oauth2.WithRedirectURL("/cb"), oauth2.WithOnLogin(func(_ context.Context, _ oauth2.Identity) (oauth2.SessionData, error) {
				return oauth2.SessionData{}, nil
			})},
			wantErr: "session store",
		},
		{
			name:    "missing onLogin",
			opts:    []oauth2.Option{oauth2.WithProvider(&mockProvider{name: "mock"}), oauth2.WithRedirectURL("/cb"), oauth2.WithSessionStore(store.NewMemoryStore())},
			wantErr: "OnLogin",
		},
		{
			name: "missing redirectURL",
			opts: []oauth2.Option{oauth2.WithProvider(&mockProvider{name: "mock"}), oauth2.WithSessionStore(store.NewMemoryStore()), oauth2.WithOnLogin(func(_ context.Context, _ oauth2.Identity) (oauth2.SessionData, error) {
				return oauth2.SessionData{}, nil
			})},
			wantErr: "redirect URL",
		},
		{
			name: "missing provider",
			opts: []oauth2.Option{oauth2.WithRedirectURL("/cb"), oauth2.WithSessionStore(store.NewMemoryStore()), oauth2.WithOnLogin(func(_ context.Context, _ oauth2.Identity) (oauth2.SessionData, error) {
				return oauth2.SessionData{}, nil
			})},
			wantErr: "provider",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := oauth2.New(tc.opts...)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestHandleLogin_redirectsToProvider(t *testing.T) {
	t.Parallel()

	m := newTestManager(t)
	handler := m.Routes()

	req := httptest.NewRequest(http.MethodGet, "/mock/login", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("expected 307, got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	if location == "" {
		t.Fatal("expected Location header")
	}

	// State cookie must be set.
	var hasStateCookie bool

	for _, c := range rec.Result().Cookies() {
		if strings.Contains(c.Name, "state") {
			hasStateCookie = true
		}
	}

	if !hasStateCookie {
		t.Error("expected state cookie to be set")
	}
}

func TestHandleLogin_unknownProviderReturns400(t *testing.T) {
	t.Parallel()

	m := newTestManager(t)

	req := httptest.NewRequest(http.MethodGet, "/unknown/login", http.NoBody)
	rec := httptest.NewRecorder()
	m.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleCallback_missingStateReturns400(t *testing.T) {
	t.Parallel()

	m := newTestManager(t)

	req := httptest.NewRequest(http.MethodGet, "/mock/callback?code=abc&state=invalid", http.NoBody)
	rec := httptest.NewRecorder()
	m.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleLogout(t *testing.T) {
	t.Parallel()

	m := newTestManager(t)

	req := httptest.NewRequest(http.MethodGet, "/logout", http.NoBody)
	rec := httptest.NewRecorder()
	m.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rec.Code)
	}

	if rec.Header().Get("Location") != "/" {
		t.Errorf("expected redirect to /, got %q", rec.Header().Get("Location"))
	}
}

func TestRequireAuth_noSession(t *testing.T) {
	t.Parallel()

	m := newTestManager(t)

	handler := m.RequireAuth()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should redirect to login, not call the handler.
	if rec.Code == http.StatusOK {
		t.Error("expected redirect, got 200")
	}
}

func TestLoadSession_noSessionCallsHandler(t *testing.T) {
	t.Parallel()

	m := newTestManager(t)

	var handlerCalled bool

	handler := m.LoadSession()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true

		_, hasSession := oauth2.SessionFromContext(r.Context())
		if hasSession {
			t.Error("expected no session in context for unauthenticated request")
		}

		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !handlerCalled {
		t.Error("expected handler to be called")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
