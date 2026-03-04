// Package jwt provides JWT generation, validation middleware, and context
// helpers built on top of go-jose/go-jose/v4.
//
// Claims are generic — embed [github.com/go-jose/go-jose/v4/jwt.Claims] and
// add your own fields:
//
//	type AppClaims struct {
//	    josejwt.Claims
//	    TenantID string `json:"tenant_id"`
//	    Role     string `json:"role"`
//	}
//
// Generate a token:
//
//	token, err := jwt.Generate(AppClaims{
//	    Claims:   josejwt.Claims{Subject: "user-123", Expiry: josejwt.NewNumericDate(time.Now().Add(time.Hour))},
//	    TenantID: "acme",
//	    Role:     "admin",
//	}, jwt.WithHMACKey(jwt.HS256, secret))
//
// Validate via middleware:
//
//	r.Use(jwt.Middleware[AppClaims](
//	    jwt.WithHMACKey(jwt.HS256, secret),
//	))
//
// Extract claims in a handler:
//
//	claims, ok := jwt.ClaimsFromContext[AppClaims](r.Context())
package jwt

import (
	"slices"

	"github.com/go-jose/go-jose/v4"
)

// Algorithm represents a supported JWT signing algorithm.
type Algorithm = jose.SignatureAlgorithm

const (
	// HMAC algorithms — symmetric, single shared secret.

	HS256 Algorithm = jose.HS256
	HS384 Algorithm = jose.HS384
	HS512 Algorithm = jose.HS512

	// RSA algorithms — asymmetric, private key signs, public key verifies.

	RS256 Algorithm = jose.RS256
	RS384 Algorithm = jose.RS384
	RS512 Algorithm = jose.RS512

	// ECDSA algorithms — asymmetric, smaller keys than RSA.

	ES256 Algorithm = jose.ES256
	ES384 Algorithm = jose.ES384
	ES512 Algorithm = jose.ES512
)

// KeyProvider is the interface that supplies signing and verification keys.
// Implement this interface to support custom key sources (e.g. JWKS rotation,
// HashiCorp Vault, AWS KMS).
type KeyProvider interface {
	// SigningKey returns the key used to sign new tokens and the algorithm to use.
	SigningKey() (key any, alg Algorithm, err error)

	// VerificationKeys returns the set of keys accepted for verification, along
	// with the algorithms each key may be used with.
	// Multiple keys allow for key rotation — old tokens remain valid while new
	// ones are signed with the latest key.
	VerificationKeys() (keys []VerificationKey, err error)
}

// VerificationKey pairs a verification key with its accepted algorithms.
type VerificationKey struct {
	Key        any
	Algorithms []Algorithm
}

// allowsAlgorithm reports whether this key may be used to verify a token
// signed with alg. An empty Algorithms list means "accept any algorithm".
func (vk VerificationKey) allowsAlgorithm(alg Algorithm) bool {
	if len(vk.Algorithms) == 0 {
		return true
	}

	if slices.Contains(vk.Algorithms, alg) {
		return true
	}

	return false
}
