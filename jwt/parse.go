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
//
// Two-phase algorithm enforcement (go-jose v4 requirement):
//  1. ParseSigned receives the union of all accepted algorithms so go-jose can
//     parse the header without rejecting it upfront.
//  2. During verification each key is only tried if its own Algorithms list
//     contains the token's actual alg header — an empty Algorithms list means
//     "accept any algorithm" for that key.
//
//nolint:cyclop
func parse[C any](tokenStr string, provider KeyProvider, out *C) error {
	verKeys, err := provider.VerificationKeys()
	if err != nil {
		return fmt.Errorf("get verification keys: %w", err)
	}

	// Phase 1 — build the union allowlist for ParseSigned.
	// go-jose v4 rejects tokens whose alg is not in this list before we even
	// reach signature verification.
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

	// Extract the algorithm declared in the token header.
	// tok.Headers contains one entry per signature — we only issue single-sig tokens.
	if len(tok.Headers) == 0 {
		return errors.New("token has no headers")
	}

	tokenAlg := Algorithm(tok.Headers[0].Algorithm)

	// Phase 2 — per-key verification with per-key algorithm enforcement.
	// Skip any key whose Algorithms list is non-empty and does not contain the
	// token's alg. An empty list means the key accepts any algorithm.
	var lastErr error

	for _, vk := range verKeys {
		if !vk.allowsAlgorithm(tokenAlg) {
			continue
		}

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

func validateClaims(claims any) error {
	if v, ok := claims.(josejwt.Claims); ok {
		return v.Validate(josejwt.Expected{})
	}

	return nil
}
