package jwt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// JWKSOption is a functional option for [NewJWKSProvider].
type JWKSOption func(*jwksOptions)

type jwksOptions struct {
	refreshInterval time.Duration
	httpClient      *http.Client
	algorithms      []Algorithm
}

// WithJWKSRefreshInterval sets how often the JWKS is refreshed from the
// remote endpoint. Defaults to 1 hour.
func WithJWKSRefreshInterval(d time.Duration) JWKSOption {
	return func(o *jwksOptions) {
		o.refreshInterval = d
	}
}

// WithJWKSHTTPClient sets the HTTP client used to fetch the JWKS.
// Defaults to a client with a 10s timeout.
func WithJWKSHTTPClient(client *http.Client) JWKSOption {
	return func(o *jwksOptions) {
		o.httpClient = client
	}
}

// WithJWKSAlgorithms restricts which algorithms are accepted from the JWKS.
// Defaults to RS256, RS384, RS512, ES256, ES384, ES512.
func WithJWKSAlgorithms(algs ...Algorithm) JWKSOption {
	return func(o *jwksOptions) {
		o.algorithms = algs
	}
}

// jwksProvider is a KeyProvider backed by a remote JWKS endpoint.
// It fetches and caches the key set, refreshing at a configurable interval.
// This is the correct approach for Keycloak, Auth0, Cognito, and any other
// identity provider that rotates keys.
type jwksProvider struct {
	url       string
	opts      jwksOptions
	mu        sync.RWMutex
	cached    *jose.JSONWebKeySet
	fetchedAt time.Time
}

// NewJWKSProvider returns a KeyProvider that fetches verification keys from a
// remote JWKS endpoint. It is suitable for use with Keycloak, Auth0, AWS
// Cognito, or any OpenID Connect provider.
//
// The key set is fetched lazily on first use and refreshed at the configured
// interval. Tokens are signed by the identity provider — Generate is not
// supported and will return an error.
//
// Example:
//
//	provider, err := jwt.NewJWKSProvider(
//	    "https://keycloak.company.com/realms/myrealm/protocol/openid-connect/certs",
//	    jwt.WithJWKSRefreshInterval(30 * time.Minute),
//	)
func NewJWKSProvider(url string, opts ...JWKSOption) (*jwksProvider, error) {
	if url == "" {
		return nil, errors.New("duck/jwt: JWKS URL must not be empty")
	}

	o := jwksOptions{
		refreshInterval: time.Hour,
		httpClient:      &http.Client{Timeout: 10 * time.Second},
		algorithms:      []Algorithm{RS256, RS384, RS512, ES256, ES384, ES512},
	}

	for _, opt := range opts {
		opt(&o)
	}

	return &jwksProvider{url: url, opts: o}, nil
}

// SigningKey is not supported for JWKS providers — tokens are signed by the
// identity provider, not by this application.
func (p *jwksProvider) SigningKey() (any, Algorithm, error) {
	return nil, "", errors.New("duck/jwt: JWKS provider does not support signing — tokens must be issued by the identity provider")
}

// VerificationKeys returns the current key set from the JWKS endpoint,
// refreshing if the cache has expired.
func (p *jwksProvider) VerificationKeys() ([]VerificationKey, error) {
	p.mu.RLock()
	cached := p.cached
	fetchedAt := p.fetchedAt
	p.mu.RUnlock()

	if cached == nil || time.Since(fetchedAt) > p.opts.refreshInterval {
		if err := p.refresh(); err != nil {
			if cached != nil {
				// Serve stale keys rather than failing all requests on a
				// temporary network issue.
				return p.keysFromJWKS(cached), nil
			}

			return nil, fmt.Errorf("fetch JWKS: %w", err)
		}

		p.mu.RLock()
		cached = p.cached
		p.mu.RUnlock()
	}

	return p.keysFromJWKS(cached), nil
}

// refresh fetches the JWKS from the remote endpoint and updates the cache.
func (p *jwksProvider) refresh() error {
	ctx, cancel := context.WithTimeout(context.Background(), p.opts.httpClient.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, http.NoBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := p.opts.httpClient.Do(req) //nolint:gosec // G704: URL is provided by the operator at construction time, not by end users
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from JWKS endpoint", resp.StatusCode)
	}

	var jwks jose.JSONWebKeySet
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("decode JWKS: %w", err)
	}

	p.mu.Lock()
	p.cached = &jwks
	p.fetchedAt = time.Now()
	p.mu.Unlock()

	return nil
}

// keysFromJWKS converts a JWKS into VerificationKeys accepted by our interface.
func (p *jwksProvider) keysFromJWKS(jwks *jose.JSONWebKeySet) []VerificationKey {
	keys := make([]VerificationKey, 0, len(jwks.Keys))

	for i := range jwks.Keys {
		keys = append(keys, VerificationKey{
			Key:        jwks.Keys[i].Key,
			Algorithms: p.opts.algorithms,
		})
	}

	return keys
}
