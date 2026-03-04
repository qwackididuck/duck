package jwt

import (
	"errors"
	"fmt"
)

// multiKeyProvider is a KeyProvider that combines multiple providers.
// The first provider is used for signing; all providers are tried for
// verification. This enables key rotation — old tokens remain valid while
// new ones use the current key.
type multiKeyProvider struct {
	providers []KeyProvider
}

// NewMultiKeyProvider returns a KeyProvider that uses the first provider for
// signing and tries all providers in order for verification.
//
// This is the correct pattern for key rotation:
//
//	provider := jwt.NewMultiKeyProvider(
//	    jwt.WithHMACKey(jwt.HS256, newSecret), // primary — used for signing
//	    jwt.WithHMACKey(jwt.HS256, oldSecret), // legacy — accepted for verification only
//	)
func NewMultiKeyProvider(primary KeyProvider, fallbacks ...KeyProvider) KeyProvider {
	providers := make([]KeyProvider, 0, 1+len(fallbacks))
	providers = append(providers, primary)
	providers = append(providers, fallbacks...)

	return &multiKeyProvider{providers: providers}
}

func (m *multiKeyProvider) SigningKey() (any, Algorithm, error) {
	if len(m.providers) == 0 || m.providers[0] == nil {
		return nil, "", errors.New("duck/jwt: primary provider is nil")
	}

	return m.providers[0].SigningKey()
}

func (m *multiKeyProvider) VerificationKeys() ([]VerificationKey, error) {
	var all []VerificationKey

	for i, p := range m.providers {
		if p == nil {
			return nil, fmt.Errorf("provider %d is nil", i)
		}

		keys, err := p.VerificationKeys()
		if err != nil {
			return nil, fmt.Errorf("provider %d verification keys: %w", i, err)
		}

		all = append(all, keys...)
	}

	return all, nil
}
