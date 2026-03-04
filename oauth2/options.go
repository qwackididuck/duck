package oauth2

import (
	"net/http"
	"time"
)

// Option is a functional option for [New].
type Option func(*options)

type options struct {
	providers         []Provider
	redirectURL       string
	store             SessionStore
	onLogin           OnLoginFunc
	successRedirect   string
	logoutRedirect    string
	httpClient        *http.Client
	sessionCookieName string
	stateCookieName   string
	sessionTTL        time.Duration
	secureCookies     bool
}

// WithProvider registers an OAuth2 provider (Google, GitHub, etc.).
// Multiple providers can be registered.
func WithProvider(p Provider) Option {
	return func(o *options) {
		o.providers = append(o.providers, p)
	}
}

// WithRedirectURL sets the OAuth2 callback URL.
// Use {provider} as a placeholder for the provider name:
//
//	oauth2.WithRedirectURL("https://myapp.com/auth/{provider}/callback")
func WithRedirectURL(url string) Option {
	return func(o *options) {
		o.redirectURL = url
	}
}

// WithSessionStore sets the session store used to persist sessions.
// Use [store.NewMemoryStore] for development or [store.NewRedisStore] for production.
func WithSessionStore(s SessionStore) Option {
	return func(o *options) {
		o.store = s
	}
}

// WithOnLogin sets the hook called after every successful OAuth callback.
// The hook receives the provider identity and must return the session data to
// store. This is where application logic lives: upsert user, check bans, etc.
func WithOnLogin(fn OnLoginFunc) Option {
	return func(o *options) {
		o.onLogin = fn
	}
}

// WithSuccessRedirect sets the URL to redirect to after a successful login.
// Defaults to "/".
func WithSuccessRedirect(url string) Option {
	return func(o *options) {
		o.successRedirect = url
	}
}

// WithLogoutRedirect sets the URL to redirect to after logout.
// Defaults to "/".
func WithLogoutRedirect(url string) Option {
	return func(o *options) {
		o.logoutRedirect = url
	}
}

// WithHTTPClient sets the HTTP client used for token exchange and userinfo
// requests. Defaults to http.DefaultClient. Use duck/httpclient to add
// retry and logging.
func WithHTTPClient(client *http.Client) Option {
	return func(o *options) {
		o.httpClient = client
	}
}

// WithSessionTTL sets how long sessions remain valid.
// Defaults to 7 days.
func WithSessionTTL(d time.Duration) Option {
	return func(o *options) {
		o.sessionTTL = d
	}
}

// WithSecureCookies enables the Secure flag on cookies.
// Should be true in production (HTTPS). Defaults to false for local dev.
func WithSecureCookies(secure bool) Option {
	return func(o *options) {
		o.secureCookies = secure
	}
}

// httpClient returns the configured HTTP client or http.DefaultClient.
func (o *options) getHTTPClient() *http.Client {
	if o.httpClient != nil {
		return o.httpClient
	}

	return http.DefaultClient
}

// sessionTTLOrDefault returns the session TTL or 7 days if not set.
func (o *options) sessionTTLOrDefault() time.Duration {
	if o.sessionTTL > 0 {
		return o.sessionTTL
	}

	return 7 * 24 * time.Hour
}

// contextKey is the unexported context key type for this package.
type contextKey struct{ name string }

var sessionContextKey = &contextKey{"session"}
