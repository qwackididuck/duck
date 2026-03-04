package jwt

import "context"

// contextKey is the unexported key type for context values in this package.
type contextKey struct{}

// contextValue wraps claims as any so ClaimsFromContext can be generic.
type contextValue struct {
	claims any
}

// contextWithClaims returns a new context carrying the provided claims.
func contextWithClaims(ctx context.Context, claims any) context.Context {
	return context.WithValue(ctx, contextKey{}, contextValue{claims: claims})
}

// ClaimsFromContext extracts the JWT claims from the context.
// Returns the claims and true if present, or the zero value and false if not.
//
// The type parameter C must match the type used when creating the middleware.
//
//	claims, ok := jwt.ClaimsFromContext[AppClaims](r.Context())
//	if !ok {
//	    // route is not protected or middleware was not applied
//	}
func ClaimsFromContext[C any](ctx context.Context) (C, bool) {
	v, ok := ctx.Value(contextKey{}).(contextValue)
	if !ok {
		var zero C

		return zero, false
	}

	claims, ok := v.claims.(C)

	return claims, ok
}
