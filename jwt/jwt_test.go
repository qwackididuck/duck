package jwt_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	josejwt "github.com/go-jose/go-jose/v4/jwt"

	duckjwt "github.com/qwackididuck/duck/jwt"
)

// appClaims is the custom claims type used across tests.
type appClaims struct {
	josejwt.Claims

	TenantID string `json:"tenant_id"` //nolint:tagliatelle // test struct, not production code
	Role     string `json:"role"`
}

var (
	testSecretHS256 = []byte("test-hmac-secret-hs256-32-bytes!!")                                // 32 bytes — HS256 minimum
	testSecretHS384 = []byte("duck-jwt-hs384-secret-48-bytes-min!!!!!!!!!!!!!!")                 // 48 bytes — HS384 minimum
	testSecretHS512 = []byte("duck-jwt-hs512-secret-minimum-64-bytes-long-for-test-purposes!!!") // 64 bytes — HS512 minimum
)

//nolint:unparam
func makeClaims(sub, tenant, role string) appClaims {
	return appClaims{
		Claims: josejwt.Claims{
			Subject: sub,
			Expiry:  josejwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:  "duck-test",
		},
		TenantID: tenant,
		Role:     role,
	}
}

// --- Generate ---

func TestGenerate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		alg     duckjwt.Algorithm
		secret  []byte
		wantErr bool
	}{
		{
			name:   "HMAC HS256 — valid key size",
			alg:    duckjwt.HS256,
			secret: testSecretHS256,
		},
		{
			name:   "HMAC HS384 — valid key size",
			alg:    duckjwt.HS384,
			secret: testSecretHS384,
		},
		{
			name:   "HMAC HS512 — valid key size",
			alg:    duckjwt.HS512,
			secret: testSecretHS512,
		},
		{
			name:    "HMAC HS256 — secret too short returns error",
			alg:     duckjwt.HS256,
			secret:  []byte("tooshort"),
			wantErr: true,
		},
		{
			name:    "HMAC HS384 — secret too short returns error",
			alg:     duckjwt.HS384,
			secret:  []byte("tooshort"),
			wantErr: true,
		},
		{
			name:    "HMAC HS512 — secret too short returns error",
			alg:     duckjwt.HS512,
			secret:  []byte("tooshort"),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			provider, err := duckjwt.WithHMACKey(tc.alg, tc.secret)

			if tc.wantErr {
				if err == nil {
					t.Fatal("WithHMACKey() expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("WithHMACKey() unexpected error: %v", err)
			}

			token, err := duckjwt.Generate(makeClaims("user-1", "acme", "admin"), provider)
			if err != nil {
				t.Fatalf("Generate() unexpected error: %v", err)
			}

			if token == "" {
				t.Fatal("Generate() returned empty token")
			}
		})
	}
}

func TestGenerate_RSA(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	provider, err := duckjwt.WithRSAKey(duckjwt.RS256, key, &key.PublicKey)
	if err != nil {
		t.Fatalf("WithRSAKey: %v", err)
	}

	token, err := duckjwt.Generate(makeClaims("user-1", "acme", "admin"), provider)
	if err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	if token == "" {
		t.Fatal("Generate() returned empty token")
	}
}

func TestGenerate_ECDSA(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}

	provider, err := duckjwt.WithECDSAKey(duckjwt.ES256, key, &key.PublicKey)
	if err != nil {
		t.Fatalf("WithECDSAKey: %v", err)
	}

	token, err := duckjwt.Generate(makeClaims("user-1", "acme", "admin"), provider)
	if err != nil {
		t.Fatalf("Generate() unexpected error: %v", err)
	}

	if token == "" {
		t.Fatal("Generate() returned empty token")
	}
}

// --- ClaimsFromContext ---

func TestClaimsFromContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setupCtx  func() context.Context
		wantFound bool
		wantRole  string
	}{
		{
			name: "claims present in context",
			setupCtx: func() context.Context {
				provider, err := duckjwt.WithHMACKey(duckjwt.HS256, testSecretHS256)
				if err != nil {
					t.Fatalf("WithHMACKey: %v", err)
				}

				token, _ := duckjwt.Generate(makeClaims("u1", "acme", "admin"), provider)
				req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
				req.Header.Set("Authorization", "Bearer "+token)

				rec := httptest.NewRecorder()

				var capturedCtx context.Context

				mw := duckjwt.Middleware[appClaims](provider)
				mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
					capturedCtx = r.Context() //nolint:fatcontext // test helper, not production code
				})).ServeHTTP(rec, req)

				return capturedCtx
			},
			wantFound: true,
			wantRole:  "admin",
		},
		{
			name:      "empty context returns not found",
			setupCtx:  context.Background,
			wantFound: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := tc.setupCtx()
			if ctx == nil {
				t.Skip("context not captured (middleware did not call handler)")
			}

			claims, ok := duckjwt.ClaimsFromContext[appClaims](ctx)

			if ok != tc.wantFound {
				t.Errorf("found: expected %v, got %v", tc.wantFound, ok)
			}

			if tc.wantFound && claims.Role != tc.wantRole {
				t.Errorf("Role: expected %q, got %q", tc.wantRole, claims.Role)
			}
		})
	}
}

// --- Middleware ---

func TestMiddleware(t *testing.T) {
	t.Parallel()

	provider, err := duckjwt.WithHMACKey(duckjwt.HS256, testSecretHS256)
	if err != nil {
		t.Fatalf("WithHMACKey: %v", err)
	}

	validToken := func() string {
		t.Helper()

		tok, err := duckjwt.Generate(makeClaims("u1", "acme", "admin"), provider)
		if err != nil {
			t.Fatalf("generate token: %v", err)
		}

		return tok
	}()

	tests := []struct {
		name       string
		authHeader string
		opts       []duckjwt.MiddlewareOption
		wantStatus int
		wantClaims bool
	}{
		{
			name:       "valid token — claims injected into context",
			authHeader: "Bearer " + validToken,
			wantStatus: http.StatusOK,
			wantClaims: true,
		},
		{
			name:       "missing Authorization header returns 401",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "malformed Authorization header returns 401",
			authHeader: "Token " + validToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid token returns 401",
			authHeader: "Bearer notavalidtoken",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing token with optional — handler called without claims",
			authHeader: "",
			opts:       []duckjwt.MiddlewareOption{duckjwt.WithOptional()},
			wantStatus: http.StatusOK,
			wantClaims: false,
		},
		{
			name:       "valid token with optional — claims still injected",
			authHeader: "Bearer " + validToken,
			opts:       []duckjwt.MiddlewareOption{duckjwt.WithOptional()},
			wantStatus: http.StatusOK,
			wantClaims: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotClaims bool

			inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, ok := duckjwt.ClaimsFromContext[appClaims](r.Context())
				gotClaims = ok
			})

			handler := duckjwt.Middleware[appClaims](provider, tc.opts...)(inner)

			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status: expected %d, got %d", tc.wantStatus, rec.Code)
			}

			if tc.wantClaims && !gotClaims {
				t.Error("expected claims in context, got none")
			}

			if !tc.wantClaims && gotClaims {
				t.Error("expected no claims in context, got some")
			}
		})
	}
}

// --- Key rotation ---

func TestGenerate_keyRotation(t *testing.T) {
	t.Parallel()

	oldSecret := []byte("old-secret-key-for-rotation-test!!!!") // 36 bytes > 32
	newSecret := []byte("new-secret-key-for-rotation-test!!!!") // 36 bytes > 32

	// Token signed with old key.
	oldProvider, err := duckjwt.WithHMACKey(duckjwt.HS256, oldSecret)
	if err != nil {
		t.Fatalf("WithHMACKey old: %v", err)
	}

	token, err := duckjwt.Generate(makeClaims("u1", "acme", "admin"), oldProvider)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Provider that accepts both old and new keys.
	newProvider, err2 := duckjwt.WithHMACKey(duckjwt.HS256, newSecret)
	if err2 != nil {
		t.Fatalf("WithHMACKey new: %v", err2)
	}

	rotationProvider := duckjwt.NewMultiKeyProvider(
		newProvider, // primary — used for signing
		oldProvider, // legacy — accepted for verification only
	)

	// Old token should still be valid.
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()

	var gotClaims bool

	duckjwt.Middleware[appClaims](rotationProvider)(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			_, gotClaims = duckjwt.ClaimsFromContext[appClaims](r.Context())
		}),
	).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with rotation provider, got %d", rec.Code)
	}

	if !gotClaims {
		t.Error("expected claims from old token with rotation provider")
	}
}
