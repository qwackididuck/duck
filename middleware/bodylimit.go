package middleware

import (
	"net/http"
)

const defaultBodyLimit = 1 * 1024 * 1024 // 1MB

// BodyLimit returns a middleware that rejects requests whose body exceeds
// maxBytes with a 413 Request Entity Too Large response.
//
// The limit is enforced by wrapping r.Body with an [io.LimitedReader] — the
// body is never fully read by the middleware itself, so downstream handlers
// can still stream it normally.
//
// A maxBytes value of 0 uses the default limit of 1MB.
// A negative value disables the limit entirely.
//
// Example:
//
//	mux.Handle("/upload", middleware.Chain(
//	    uploadHandler,
//	    middleware.BodyLimit(10*1024*1024), // 10MB
//	))
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	if maxBytes == 0 {
		maxBytes = defaultBodyLimit
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if maxBytes < 0 {
				next.ServeHTTP(w, r)

				return
			}

			if r.ContentLength > maxBytes {
				http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)

				return
			}

			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

			next.ServeHTTP(w, r)

			if err := r.Body.Close(); err != nil {
				// MaxBytesReader returns a *http.MaxBytesError if the limit is
				// exceeded during reading. The handler is responsible for
				// returning an appropriate response in that case — we just
				// ensure the body is closed.
				_ = err
			}
		})
	}
}
