package jwt

import (
	"errors"
	"net/http"
	"strings"
)

// MiddlewareOption is a functional option for [Middleware].
type MiddlewareOption func(*middlewareOptions)

type middlewareOptions struct {
	optional bool
}

// WithOptional makes the middleware skip authentication instead of returning
// 401 when no token is present. The handler is still called, but
// [ClaimsFromContext] will return false.
//
// Use this for routes that support both authenticated and anonymous access.
func WithOptional() MiddlewareOption {
	return func(o *middlewareOptions) {
		o.optional = true
	}
}

// Middleware returns an HTTP middleware that validates a Bearer JWT token from
// the Authorization header, parses the claims into C, and injects them into
// the request context.
//
// On success, downstream handlers can retrieve claims via [ClaimsFromContext].
//
// On failure:
//   - Missing token: 401 Unauthorized (or passthrough if [WithOptional] is set)
//   - Invalid token: 401 Unauthorized
//
// Example:
//
//	r.Use(jwt.Middleware[AppClaims](
//	    jwt.WithHMACKey(jwt.HS256, secret),
//	    jwt.WithExpectedClaims(josejwt.Expected{Issuer: "myapp"}),
//	))
func Middleware[C any](provider KeyProvider, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	o := &middlewareOptions{}
	for _, opt := range opts {
		opt(o)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr, err := extractBearer(r)
			if err != nil {
				if o.optional && errors.Is(err, errNoToken) {
					// No token provided — optional route, proceed without claims.
					next.ServeHTTP(w, r)

					return
				}

				http.Error(w, "missing or malformed Authorization header", http.StatusUnauthorized)

				return
			}

			var claims C

			if err := parse(tokenStr, provider, &claims); err != nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)

				return
			}

			next.ServeHTTP(w, r.WithContext(contextWithClaims(r.Context(), claims)))
		})
	}
}

// errNoToken is returned by extractBearer when no Authorization header is present.
var errNoToken = errors.New("no token")

// extractBearer extracts the Bearer token from the Authorization header.
func extractBearer(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", errNoToken
	}

	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", errors.New("malformed Authorization header")
	}

	return parts[1], nil
}
