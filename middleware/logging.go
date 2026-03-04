package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	ducklog "github.com/qwackididuck/duck/log"
)

const (
	headerRequestID       = "X-Request-Id"
	defaultMaxBodySize    = 1024
	obfuscatedPlaceholder = "***"
)

// loggingOptions holds the configuration for the Logging middleware.
type loggingOptions struct {
	logRequestBody   bool
	logResponseBody  bool
	maxBodySize      int64
	obfuscHeaders    map[string]struct{}
	obfuscBodyFields map[string]struct{}
}

// LoggingOption is a functional option for the Logging middleware.
type LoggingOption func(*loggingOptions)

// WithRequestBody enables logging of the request body.
func WithRequestBody(enabled bool) LoggingOption {
	return func(o *loggingOptions) {
		o.logRequestBody = enabled
	}
}

// WithResponseBody enables logging of the response body.
func WithResponseBody(enabled bool) LoggingOption {
	return func(o *loggingOptions) {
		o.logResponseBody = enabled
	}
}

// WithMaxBodySize sets the maximum number of bytes logged from a body.
// Bodies larger than this are truncated. Defaults to 1024 bytes.
func WithMaxBodySize(n int64) LoggingOption {
	return func(o *loggingOptions) {
		o.maxBodySize = n
	}
}

// WithObfuscatedHeaders sets header names whose values are replaced with
// "***" in logs. Authorization is always obfuscated. Matching is case-insensitive.
func WithObfuscatedHeaders(headers ...string) LoggingOption {
	return func(o *loggingOptions) {
		for _, h := range headers {
			o.obfuscHeaders[strings.ToLower(h)] = struct{}{}
		}
	}
}

// WithObfuscatedBodyFields sets JSON body field names whose values are
// replaced with "***" in logs. Only applies to JSON bodies.
func WithObfuscatedBodyFields(fields ...string) LoggingOption {
	return func(o *loggingOptions) {
		for _, f := range fields {
			o.obfuscBodyFields[f] = struct{}{}
		}
	}
}

// Logging returns a middleware that logs incoming requests and outgoing
// responses using the provided logger.
//
// It extracts or generates a request ID from the X-Request-Id header,
// injects it into the request context via [ducklog.ContextWithAttrs], and
// propagates it in the response header.
func Logging(logger *slog.Logger, opts ...LoggingOption) func(http.Handler) http.Handler {
	o := &loggingOptions{
		maxBodySize:      defaultMaxBodySize,
		obfuscHeaders:    map[string]struct{}{},
		obfuscBodyFields: map[string]struct{}{},
	}

	for _, opt := range opts {
		opt(o)
	}

	o.obfuscHeaders[strings.ToLower("Authorization")] = struct{}{}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			requestID := r.Header.Get(headerRequestID)
			if requestID == "" {
				requestID = generateRequestID()
			}

			ctx := ducklog.ContextWithAttrs(r.Context(),
				slog.String("request_id", requestID),
			)
			r = r.WithContext(ctx)

			w.Header().Set(headerRequestID, requestID)

			reqAttrs := buildRequestAttrs(r, o)
			ducklog.FromContext(ctx, logger).Info("incoming request", reqAttrs...)

			rw := newResponseWriter(w, o.logResponseBody, o.maxBodySize)
			next.ServeHTTP(rw, r)

			respAttrs := buildResponseAttrs(r, rw, w, o, time.Since(start))
			ducklog.FromContext(ctx, logger).Info("outgoing response", respAttrs...)
		})
	}
}

// buildRequestAttrs assembles the slog attributes for an incoming request log line.
func buildRequestAttrs(r *http.Request, o *loggingOptions) []any {
	attrs := []any{
		slog.String("event_type", "incoming_request"),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	}

	if q := r.URL.RawQuery; q != "" {
		attrs = append(attrs, slog.String("query", q))
	}

	attrs = append(attrs, slog.Any("headers", sanitizeHeaders(r.Header, o.obfuscHeaders)))

	if o.logRequestBody {
		body, err := readAndRestoreBody(r, o.maxBodySize)
		if err == nil && body != "" {
			attrs = append(attrs, slog.String("body", obfuscateBody(body, o.obfuscBodyFields)))
		}
	}

	return attrs
}

// buildResponseAttrs assembles the slog attributes for an outgoing response log line.
func buildResponseAttrs(r *http.Request, rw *responseWriter, w http.ResponseWriter, o *loggingOptions, duration time.Duration) []any {
	attrs := []any{
		slog.String("event_type", "outgoing_response"),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int64("duration_ms", duration.Milliseconds()),
	}

	if rw.statusWritten {
		attrs = append(attrs, slog.Int("status", rw.status))
	}

	attrs = append(attrs, slog.Any("headers", sanitizeHeaders(w.Header(), o.obfuscHeaders)))

	if o.logResponseBody && rw.body.Len() > 0 {
		attrs = append(attrs, slog.String("body", obfuscateBody(rw.body.String(), o.obfuscBodyFields)))
	}

	return attrs
}

// responseWriter wraps http.ResponseWriter to capture status code and
// optionally buffer the response body for logging.
type responseWriter struct {
	http.ResponseWriter

	status        int
	statusWritten bool
	body          bytes.Buffer
	captureBody   bool
	maxBodySize   int64
	written       int64
}

func newResponseWriter(w http.ResponseWriter, captureBody bool, maxBodySize int64) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		captureBody:    captureBody,
		maxBodySize:    maxBodySize,
	}
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.statusWritten = true
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.captureBody && rw.written < rw.maxBodySize {
		remaining := rw.maxBodySize - rw.written
		if int64(len(b)) < remaining {
			rw.body.Write(b)
		} else {
			rw.body.Write(b[:remaining])
		}

		rw.written += int64(len(b))
	}

	return rw.ResponseWriter.Write(b)
}

// readAndRestoreBody reads up to maxBytes from r.Body and restores it
// so downstream handlers can read it again.
func readAndRestoreBody(r *http.Request, maxBytes int64) (string, error) {
	if r.Body == nil {
		return "", nil
	}

	data, err := io.ReadAll(io.LimitReader(r.Body, maxBytes))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(data), r.Body))

	return string(data), nil
}

// sanitizeHeaders returns a flat map of headers with sensitive values masked.
func sanitizeHeaders(h http.Header, obfusc map[string]struct{}) map[string]string {
	result := make(map[string]string, len(h))

	for k, v := range h {
		if _, masked := obfusc[strings.ToLower(k)]; masked {
			result[k] = obfuscatedPlaceholder
		} else {
			result[k] = strings.Join(v, ", ")
		}
	}

	return result
}

// obfuscateBody replaces sensitive fields in a JSON body with "***".
// If the body is not valid JSON, it is returned as-is.
func obfuscateBody(body string, fields map[string]struct{}) string {
	if len(fields) == 0 {
		return body
	}

	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return body
	}

	for field := range fields {
		if _, ok := raw[field]; ok {
			raw[field] = json.RawMessage(`"***"`)
		}
	}

	result, err := json.Marshal(raw)
	if err != nil {
		return body
	}

	return string(result)
}
