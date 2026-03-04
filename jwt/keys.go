package jwt

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"errors"
	"fmt"

	"github.com/go-jose/go-jose/v4"
)

// staticKeyProvider is a KeyProvider backed by a fixed key — for HMAC secrets
// and static RSA/ECDSA key pairs.
type staticKeyProvider struct {
	signKey any
	verKey  any
	alg     Algorithm
}

func (s *staticKeyProvider) SigningKey() (any, Algorithm, error) {
	return s.signKey, s.alg, nil
}

func (s *staticKeyProvider) VerificationKeys() ([]VerificationKey, error) {
	return []VerificationKey{
		{Key: s.verKey, Algorithms: []Algorithm{s.alg}},
	}, nil
}

// WithHMACKey returns a KeyProvider that uses a symmetric HMAC secret.
// The same secret is used for both signing and verification.
// alg must be one of HS256, HS384, or HS512.
// hmacMinKeySize maps each HMAC algorithm to its minimum required key size in
// bytes, as required by the HMAC spec and enforced by go-jose v4.
var hmacMinKeySize = map[Algorithm]int{
	HS256: 32,
	HS384: 48,
	HS512: 64,
}

// WithHMACKey returns a KeyProvider that uses a symmetric HMAC secret.
// The same secret is used for both signing and verification.
// alg must be one of HS256, HS384, or HS512.
//
// Returns an error if the secret is too short for the requested algorithm:
//   - HS256: minimum 32 bytes
//   - HS384: minimum 48 bytes
//   - HS512: minimum 64 bytes
func WithHMACKey(alg Algorithm, secret []byte) (KeyProvider, error) {
	minSize, ok := hmacMinKeySize[alg]
	if !ok {
		return nil, fmt.Errorf("duck/jwt: unsupported HMAC algorithm %q", alg)
	}

	if len(secret) < minSize {
		return nil, fmt.Errorf("duck/jwt: secret too short for %s: got %d bytes, need at least %d", alg, len(secret), minSize)
	}

	return &staticKeyProvider{
		signKey: secret,
		verKey:  secret,
		alg:     alg,
	}, nil
}

// WithRSAKey returns a KeyProvider that uses an RSA key pair.
// privateKey is required for signing; it may be nil for verification-only use.
// publicKey is used for verification.
// alg must be one of RS256, RS384, or RS512.
func WithRSAKey(alg Algorithm, privateKey *rsa.PrivateKey, publicKey *rsa.PublicKey) (KeyProvider, error) {
	if privateKey == nil && publicKey == nil {
		return nil, errors.New("duck/jwt: at least one of privateKey or publicKey must be provided")
	}

	var signKey any
	if privateKey != nil {
		signKey = privateKey
	}

	var verKey any = publicKey
	if publicKey == nil && privateKey != nil {
		verKey = &privateKey.PublicKey
	}

	return &staticKeyProvider{
		signKey: signKey,
		verKey:  verKey,
		alg:     alg,
	}, nil
}

// WithECDSAKey returns a KeyProvider that uses an ECDSA key pair.
// privateKey is required for signing; it may be nil for verification-only use.
// publicKey is used for verification.
// alg must be one of ES256, ES384, or ES512.
func WithECDSAKey(alg Algorithm, privateKey *ecdsa.PrivateKey, publicKey *ecdsa.PublicKey) (KeyProvider, error) {
	if privateKey == nil && publicKey == nil {
		return nil, errors.New("duck/jwt: at least one of privateKey or publicKey must be provided")
	}

	var signKey any
	if privateKey != nil {
		signKey = privateKey
	}

	var verKey any = publicKey
	if publicKey == nil && privateKey != nil {
		verKey = &privateKey.PublicKey
	}

	return &staticKeyProvider{
		signKey: signKey,
		verKey:  verKey,
		alg:     alg,
	}, nil
}

// Generate creates a signed JWT token from the provided claims.
// claims must be JSON-serializable. Embed [josejwt.Claims] for standard
// registered claims (sub, exp, iss, aud, etc.).
func Generate(claims any, provider KeyProvider) (string, error) {
	signKey, alg, err := provider.SigningKey()
	if err != nil {
		return "", fmt.Errorf("duck/jwt: get signing key: %w", err)
	}

	if signKey == nil {
		return "", errors.New("duck/jwt: signing key is nil — provider may be verification-only")
	}

	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: alg, Key: signKey},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		return "", fmt.Errorf("duck/jwt: create signer: %w", err)
	}

	token, err := jwtSigned(sig, claims)
	if err != nil {
		return "", fmt.Errorf("duck/jwt: sign token: %w", err)
	}

	return token, nil
}
