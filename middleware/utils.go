package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"
)

// generateRequestID generates a cryptographically random 16-byte hex string
// suitable for use as a request ID when none is provided by the caller.
//
//nolint:mnd // 16 bytes is a common length for request IDs, and hex encoding is widely used for readability.
func generateRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback keeps IDs non-empty and reduces collision risk if CSPRNG fails.
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}

	return hex.EncodeToString(b)
}

// Chain applies middlewares to a handler in order, so that the first
// middleware in the list is the outermost wrapper (first to execute on
// incoming requests, last to execute on outgoing responses).
//
// Example:
//
//	mux.Handle("/users", middleware.Chain(
//	    myHandler,
//	    middleware.Logging(logger),
//	    middleware.JWT(secret),
//	))
func Chain(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	// Apply in reverse so the first middleware is outermost.
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}

	return h
}
