// Example: JWT generation, validation middleware, key rotation, and JWKS.
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	"github.com/qwackididuck/duck/jwt"
)

// AppClaims is the application-specific JWT claims type.
// Embed josejwt.Claims for standard registered claims (sub, exp, iss, aud...).
type AppClaims struct {
	josejwt.Claims

	TenantID string `json:"tenantId"`
	Role     string `json:"role"`
}

func main() {
	// --- HMAC (symmetric) — simplest setup ---
	secret := []byte("your-32-byte-minimum-hmac-secret!!")

	provider, err := jwt.WithHMACKey(jwt.HS256, secret)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "key setup: %v\n", err)

		os.Exit(1)
	}

	// Generate a token
	claims := AppClaims{
		Claims: josejwt.Claims{
			Subject: "user-123",
			Expiry:  josejwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:  "my-service",
		},
		TenantID: "acme",
		Role:     "admin",
	}

	token, err := jwt.Generate(claims, provider)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("token: %s...\n\n", token[:40])

	// --- Middleware ---

	r := chi.NewRouter()

	// Protected route — returns 401 if token is missing or invalid
	r.With(jwt.Middleware[AppClaims](provider)).
		Get("/protected", func(w http.ResponseWriter, r *http.Request) {
			c, _ := jwt.ClaimsFromContext[AppClaims](r.Context())
			fmt.Fprintf(w, "hello %s (role: %s, tenant: %s)", c.Subject, c.Role, c.TenantID)
		})

	// Optionally authenticated route — handler always called
	r.With(jwt.Middleware[AppClaims](provider, jwt.WithOptional())).
		Get("/public", func(w http.ResponseWriter, r *http.Request) {
			if c, ok := jwt.ClaimsFromContext[AppClaims](r.Context()); ok {
				fmt.Fprintf(w, "hello %s", c.Subject)
			} else {
				fmt.Fprint(w, "hello anonymous")
			}
		})

	// Test the protected route
	req := httptest.NewRequest(http.MethodGet, "/protected", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	fmt.Printf("/protected with valid token: %d — %s\n", rec.Code, rec.Body.String())

	req = httptest.NewRequest(http.MethodGet, "/protected", http.NoBody)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	fmt.Printf("/protected without token:    %d\n\n", rec.Code)

	// --- Key rotation ---
	fmt.Println("=== Key rotation ===")

	newSecret := []byte("new-32-byte-minimum-hmac-secret!!")
	oldSecret := []byte("old-32-byte-minimum-hmac-secret!!")

	oldProvider, _ := jwt.WithHMACKey(jwt.HS256, oldSecret)
	newProvider, _ := jwt.WithHMACKey(jwt.HS256, newSecret)

	// Token signed with old key
	oldToken, _ := jwt.Generate(claims, oldProvider)

	// Multi-key provider: signs with new key, accepts both for verification
	rotationProvider := jwt.NewMultiKeyProvider(newProvider, oldProvider)

	req = httptest.NewRequest(http.MethodGet, "/protected", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+oldToken)

	rec = httptest.NewRecorder()

	r2 := chi.NewRouter()
	r2.With(jwt.Middleware[AppClaims](rotationProvider)).
		Get("/protected", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	r2.ServeHTTP(rec, req)
	fmt.Printf("old token with rotation provider: %d (expected 200)\n\n", rec.Code)

	// --- JWKS (Keycloak, Auth0, Cognito) ---
	fmt.Println("=== JWKS provider ===")

	jwksProvider, err := jwt.NewJWKSProvider(
		"https://keycloak.company.com/realms/myrealm/protocol/openid-connect/certs",
		jwt.WithJWKSRefreshInterval(30*time.Minute),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jwks: %v\n", err)
		os.Exit(1)
	}

	// Use exactly like any other provider
	_ = jwt.Middleware[AppClaims](jwksProvider)

	fmt.Println("JWKS provider ready (tokens signed by Keycloak are validated automatically)")
}
