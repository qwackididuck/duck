package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// HTTPMetricsProvider is the interface that wraps the HTTP metrics notification method.
// Implement this interface to plug in any metrics backend.
//
// NotifyServerExchange is called after each request with the status code, route,
// HTTP method, server duration, and any additional labels specific to the request.
type HTTPMetricsProvider interface {
	NotifyServerExchange(
		statusCode int,
		route, method string,
		duration time.Duration,
		additionalLabels map[string]string,
	)
}

// HTTPMetricsOption is a functional option for the HTTPMetrics middleware.
type HTTPMetricsOption func(*httpMetricsOptions)

type httpMetricsOptions struct {
	pathCleaner   func(*http.Request) string
	labelsFromCtx func(*http.Request) map[string]string
}

// WithPathCleaner sets a function that normalizes the request path into a
// low-cardinality route label. Without this, dynamic segments like /users/123
// produce a unique label per request, causing high cardinality in metrics.
//
// When using chi, a good cleaner is:
//
//	middleware.WithPathCleaner(func(r *http.Request) string {
//	    return chi.RouteContext(r.Context()).RoutePattern()
//	})
//
// Defaults to using r.URL.Path as-is.
func WithPathCleaner(fn func(*http.Request) string) HTTPMetricsOption {
	return func(o *httpMetricsOptions) {
		o.pathCleaner = fn
	}
}

// WithLabelsFromRequest sets a function that extracts additional per-request
// labels to be forwarded to NotifyServerExchange. This is the extension point
// for custom application-level labels (e.g. tenant, user tier, feature flag).
//
// The returned map must match the additionalLabels declared on the metrics
// provider at construction time.
func WithLabelsFromRequest(fn func(*http.Request) map[string]string) HTTPMetricsOption {
	return func(o *httpMetricsOptions) {
		o.labelsFromCtx = fn
	}
}

// HTTPMetrics returns a middleware that records HTTP server metrics for each
// request using the provided [HTTPMetricsProvider].
//
// By default, the full r.URL.Path is used as the route label. Use
// [WithPathCleaner] to normalise dynamic segments and avoid high cardinality.
func HTTPMetrics(provider HTTPMetricsProvider, opts ...HTTPMetricsOption) func(http.Handler) http.Handler {
	o := &httpMetricsOptions{
		pathCleaner:   func(r *http.Request) string { return r.URL.Path },
		labelsFromCtx: func(_ *http.Request) map[string]string { return nil },
	}

	for _, opt := range opts {
		opt(o)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := newResponseWriter(w, false, 0)

			next.ServeHTTP(rw, r)

			status := rw.status
			if !rw.statusWritten {
				status = http.StatusOK
			}

			provider.NotifyServerExchange(
				status,
				o.pathCleaner(r),
				restrictHTTPMethod(r.Method),
				time.Since(start),
				o.labelsFromCtx(r),
			)
		})
	}
}

// restrictHTTPMethod limits the method label to common HTTP verbs to avoid
// high cardinality from malformed or unusual requests.
func restrictHTTPMethod(method string) string {
	switch method {
	case http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodHead,
		http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}

// StatusLabel converts an HTTP status code into a low-cardinality string label.
// e.g. 200 -> "2xx", 404 -> "4xx", 500 -> "5xx".
// Use this when you want bucketed status labels instead of exact codes.
//
//nolint:mnd
func StatusLabel(code int) string {
	return strconv.Itoa(code/100) + "xx"
}

// ExactStatusLabel returns the status code as a string label.
// e.g. 200 -> "200", 404 -> "404".
func ExactStatusLabel(code int) string {
	return strconv.Itoa(code)
}

// RestrictHTTPMethod is the exported version of restrictHTTPMethod, available
// to HTTPMetricsProvider implementations that need to apply the same method
// restriction logic in their label sets.
func RestrictHTTPMethod(method string) string {
	return restrictHTTPMethod(method)
}

// JoinContentType extracts the base content type without parameters.
// e.g. "application/json; charset=utf-8" -> "application/json".
func JoinContentType(ct string) string {
	bf, _, _ := strings.Cut(ct, ";")

	return bf
}
