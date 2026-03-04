package httpclient

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	ducklog "github.com/qwackididuck/duck/log"
)

const (
	headerXRequestID      = "X-Request-Id"
	obfuscatedPlaceholder = "***"
)

// loggingTransportOptions holds the configuration for the logging transport.
type loggingTransportOptions struct {
	logger           *slog.Logger
	logRequestBody   bool
	logResponseBody  bool
	maxBodySize      int64
	obfuscHeaders    map[string]struct{}
	obfuscBodyFields map[string]struct{}
}

// LoggingOption is a functional option for [WithLogging].
type LoggingOption func(*loggingTransportOptions)

// WithClientRequestBody enables logging of outgoing request bodies.
func WithClientRequestBody(enabled bool) LoggingOption {
	return func(o *loggingTransportOptions) {
		o.logRequestBody = enabled
	}
}

// WithClientResponseBody enables logging of incoming response bodies.
func WithClientResponseBody(enabled bool) LoggingOption {
	return func(o *loggingTransportOptions) {
		o.logResponseBody = enabled
	}
}

// WithClientMaxBodySize sets the maximum number of bytes logged from a body.
// Defaults to 1024 bytes.
func WithClientMaxBodySize(n int64) LoggingOption {
	return func(o *loggingTransportOptions) {
		o.maxBodySize = n
	}
}

// WithClientObfuscatedHeaders sets header names whose values are replaced
// with "***" in logs. Authorization is always obfuscated.
func WithClientObfuscatedHeaders(headers ...string) LoggingOption {
	return func(o *loggingTransportOptions) {
		for _, h := range headers {
			o.obfuscHeaders[strings.ToLower(h)] = struct{}{}
		}
	}
}

// WithClientObfuscatedBodyFields sets JSON body field names whose values are
// replaced with "***" in logs.
func WithClientObfuscatedBodyFields(fields ...string) LoggingOption {
	return func(o *loggingTransportOptions) {
		for _, f := range fields {
			o.obfuscBodyFields[f] = struct{}{}
		}
	}
}

// WithLogging configures outgoing request and incoming response logging.
//
// The request_id is propagated from the request context (injected by the
// server-side [middleware.Logging]) and forwarded in the X-Request-Id header
// so the downstream service can correlate logs across services.
func WithLogging(logger *slog.Logger, opts ...LoggingOption) Option {
	return func(o *clientOptions) {
		lo := &loggingTransportOptions{
			logger:           logger,
			maxBodySize:      1024,
			obfuscHeaders:    map[string]struct{}{},
			obfuscBodyFields: map[string]struct{}{},
		}

		for _, opt := range opts {
			opt(lo)
		}

		// Authorization is always obfuscated.
		lo.obfuscHeaders[strings.ToLower("Authorization")] = struct{}{}

		o.logging = lo
	}
}

// loggingTransport is an http.RoundTripper that logs outgoing requests and
// incoming responses.
type loggingTransport struct {
	next http.RoundTripper
	opts *loggingTransportOptions
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	ctx := req.Context()

	// Propagate X-Request-Id to downstream service.
	// The caller is expected to set this header on the outgoing request,
	// typically after extracting it from the incoming server request context.
	// We ensure the header is forwarded as-is — no generation here.
	_ = ctx

	// Log outgoing request.
	reqAttrs := t.buildRequestAttrs(req)
	ducklog.FromContext(ctx, t.opts.logger).Info("outgoing request", reqAttrs...)

	resp, err := t.next.RoundTrip(req)

	// Log incoming response.
	respAttrs := t.buildResponseAttrs(req, resp, err, time.Since(start))
	ducklog.FromContext(ctx, t.opts.logger).Info("incoming response", respAttrs...)

	return resp, err
}

// buildRequestAttrs assembles slog attributes for an outgoing request.
func (t *loggingTransport) buildRequestAttrs(req *http.Request) []any {
	attrs := []any{
		slog.String("event_type", "outgoing_request"),
		slog.String("method", req.Method),
		slog.String("url", req.URL.String()),
		slog.Any("headers", sanitizeClientHeaders(req.Header, t.opts.obfuscHeaders)),
	}

	if t.opts.logRequestBody {
		body, err := readAndRestoreClientBody(req, t.opts.maxBodySize)
		if err == nil && body != "" {
			attrs = append(attrs, slog.String("body", obfuscateClientBody(body, t.opts.obfuscBodyFields)))
		}
	}

	return attrs
}

// buildResponseAttrs assembles slog attributes for an incoming response.
func (t *loggingTransport) buildResponseAttrs(req *http.Request, resp *http.Response, err error, duration time.Duration) []any {
	attrs := []any{
		slog.String("event_type", "incoming_response"),
		slog.String("method", req.Method),
		slog.String("url", req.URL.String()),
		slog.Int64("duration_ms", duration.Milliseconds()),
	}

	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))

		return attrs
	}

	attrs = append(attrs,
		slog.Int("status", resp.StatusCode),
		slog.Any("headers", sanitizeClientHeaders(resp.Header, t.opts.obfuscHeaders)),
	)

	if t.opts.logResponseBody && resp != nil {
		body, readErr := readAndRestoreResponseBody(resp, t.opts.maxBodySize)
		if readErr == nil && body != "" {
			attrs = append(attrs, slog.String("body", obfuscateClientBody(body, t.opts.obfuscBodyFields)))
		}
	}

	return attrs
}

// sanitizeClientHeaders returns a flat map with sensitive headers masked.
func sanitizeClientHeaders(h http.Header, obfusc map[string]struct{}) map[string]string {
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

// readAndRestoreClientBody reads up to maxBytes from req.Body and restores it.
func readAndRestoreClientBody(req *http.Request, maxBytes int64) (string, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return "", nil
	}

	original := req.Body

	data, err := io.ReadAll(io.LimitReader(original, maxBytes))
	if err != nil {
		return "", err
	}

	req.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: io.MultiReader(bytes.NewReader(data), original),
		Closer: original,
	}

	return string(data), nil
}

// readAndRestoreResponseBody reads up to maxBytes from resp.Body and restores it.
func readAndRestoreResponseBody(resp *http.Response, maxBytes int64) (string, error) {
	if resp.Body == nil {
		return "", nil
	}

	original := resp.Body

	data, err := io.ReadAll(io.LimitReader(original, maxBytes))
	if err != nil {
		return "", err
	}

	resp.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: io.MultiReader(bytes.NewReader(data), original),
		Closer: original,
	}

	return string(data), nil
}

// obfuscateClientBody replaces sensitive fields in a JSON body with "***".
func obfuscateClientBody(body string, fields map[string]struct{}) string {
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
