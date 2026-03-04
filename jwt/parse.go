package jwt

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
)

// jwtSigned builds and serializes a signed JWT from a signer and claims.
// It is a thin wrapper that keeps the go-jose builder API internal.
func jwtSigned(sig jose.Signer, claims any) (string, error) {
	return josejwt.Signed(sig).Claims(claims).Serialize()
}

// parse parses and verifies a JWT token string, extracting claims into out.
// It tries each VerificationKey from the provider in order — useful for key
// rotation where multiple valid keys may exist simultaneously.
func parse[C any](tokenStr string, provider KeyProvider, out *C) error {
	verKeys, err := provider.VerificationKeys()
	if err != nil {
		return fmt.Errorf("get verification keys: %w", err)
	}

	// Collect all accepted algorithms across all keys to pass to ParseSigned.
	// go-jose v4 requires explicit algorithm allowlist to prevent confusion attacks.
	algSet := map[Algorithm]struct{}{}

	for _, vk := range verKeys {
		for _, alg := range vk.Algorithms {
			algSet[alg] = struct{}{}
		}
	}

	algs := make([]Algorithm, 0, len(algSet))
	for alg := range algSet {
		algs = append(algs, alg)
	}

	tok, err := josejwt.ParseSigned(tokenStr, algs)
	if err != nil {
		return fmt.Errorf("parse token: %w", err)
	}

	// Try each key — the first one that verifies the signature wins.
	var lastErr error

	for _, vk := range verKeys {
		raw := json.RawMessage{}
		if err := tok.Claims(vk.Key, &raw); err != nil {
			lastErr = err

			continue
		}

		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("unmarshal claims: %w", err)
		}

		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("verify signature: %w", lastErr)
	}

	return errors.New("no verification key matched")
}
