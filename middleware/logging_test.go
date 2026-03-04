package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ducklog "github.com/qwackididuck/duck/log"
	"github.com/qwackididuck/duck/middleware"
)

// --- Helpers ---

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return ducklog.New(
		ducklog.WithFormat(ducklog.FormatJSON),
		ducklog.WithOutput(buf),
	)
}

func decodeAllLogs(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	result := make([]map[string]any, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("decodeAllLogs: %v\nraw: %s", err, line)
		}

		result = append(result, m)
	}

	return result
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func jsonHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(body))
	})
}

// --- Chain ---

func TestChain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		middlewares []func(http.Handler) http.Handler
		wantOrder   []string
	}{
		{
			name:        "no middlewares passes through",
			middlewares: nil,
			wantOrder:   []string{"handler"},
		},
		{
			name: "single middleware wraps handler",
			middlewares: []func(http.Handler) http.Handler{
				func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("X-Mw", "one")
						next.ServeHTTP(w, r)
					})
				},
			},
			wantOrder: []string{"handler"},
		},
		{
			name: "middlewares execute in declared order",
			middlewares: []func(http.Handler) http.Handler{
				func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Add("X-Order", "first")
						next.ServeHTTP(w, r)
					})
				},
				func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Add("X-Order", "second")
						next.ServeHTTP(w, r)
					})
				},
			},
			wantOrder: []string{"first", "second"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := middleware.Chain(okHandler(), tc.middlewares...)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)

			handler.ServeHTTP(rec, req)

			if tc.name == "middlewares execute in declared order" {
				order := rec.Header().Values("X-Order")
				for i, want := range tc.wantOrder {
					if i >= len(order) || order[i] != want {
						t.Errorf("order[%d]: expected %q, got %q", i, want, order)
					}
				}
			}

			if rec.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", rec.Code)
			}
		})
	}
}

// --- Logging middleware ---

func TestLogging_requestID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		requestID     string
		wantGenerated bool
	}{
		{
			name:          "uses provided X-Request-Id",
			requestID:     "my-request-id",
			wantGenerated: false,
		},
		{
			name:          "generates request ID when absent",
			requestID:     "",
			wantGenerated: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			logger := newTestLogger(&buf)
			handler := middleware.Chain(okHandler(), middleware.Logging(logger))

			req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
			if tc.requestID != "" {
				req.Header.Set("X-Request-Id", tc.requestID)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// Request ID must appear in response header.
			gotID := rec.Header().Get("X-Request-Id")
			if gotID == "" {
				t.Fatal("X-Request-Id missing from response header")
			}

			if !tc.wantGenerated && gotID != tc.requestID {
				t.Errorf("expected request ID %q, got %q", tc.requestID, gotID)
			}

			// Request ID must appear in the log.
			logs := decodeAllLogs(t, &buf)
			for _, entry := range logs {
				if entry["request_id"] != gotID {
					t.Errorf("log entry missing or wrong request_id: %v", entry)
				}
			}
		})
	}
}

func TestLogging_contextAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := newTestLogger(&buf)

	// Pre-enrich the logger context (simulates component middleware upstream).
	baseCtx := ducklog.ContextWithAttrs(context.Background(),
		slog.String("component", "api"),
	)

	handler := middleware.Chain(okHandler(), middleware.Logging(logger))
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody).WithContext(baseCtx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	for _, entry := range decodeAllLogs(t, &buf) {
		if entry["component"] != "api" {
			t.Errorf("expected component=api in log entry, got: %v", entry)
		}
	}
}

func TestLogging_logLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		handler        http.Handler
		wantEventTypes []string
		wantStatus     *int
	}{
		{
			name:           "two log lines per request",
			handler:        okHandler(),
			wantEventTypes: []string{"incoming_request", "outgoing_response"},
			wantStatus:     func() *int { return new(http.StatusOK) }(),
		},
		{
			name: "status logged when explicitly written",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}),
			wantEventTypes: []string{"incoming_request", "outgoing_response"},
			wantStatus:     func() *int { return new(http.StatusNotFound) }(),
		},
		{
			name:           "status not logged when not explicitly written",
			handler:        http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}),
			wantEventTypes: []string{"incoming_request", "outgoing_response"},
			wantStatus:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			logger := newTestLogger(&buf)

			handler := middleware.Chain(tc.handler, middleware.Logging(logger))

			req := httptest.NewRequest(http.MethodGet, "/test?page=1", http.NoBody)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			logs := decodeAllLogs(t, &buf)

			if len(logs) != len(tc.wantEventTypes) {
				t.Fatalf("expected %d log lines, got %d", len(tc.wantEventTypes), len(logs))
			}

			for i, wantType := range tc.wantEventTypes {
				if logs[i]["event_type"] != wantType {
					t.Errorf("log[%d] event_type: expected %q, got %v", i, wantType, logs[i]["event_type"])
				}
			}

			respLog := logs[1]

			if tc.wantStatus != nil {
				status, ok := respLog["status"]
				if !ok {
					t.Error("expected status field in response log, got none")
				} else if i, ok := status.(float64); ok && int(i) != *tc.wantStatus {
					t.Errorf("status: expected %d, got %v", *tc.wantStatus, status)
				}
			} else {
				if _, ok := respLog["status"]; ok {
					t.Error("status field should not be present when not explicitly written")
				}
			}
		})
	}
}

//nolint:cyclop
func TestLogging_obfuscation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		requestHeaders   map[string]string
		requestBody      string
		responseBody     string
		opts             []middleware.LoggingOption
		wantHeaderMasked string
		wantBodyMasked   string
	}{
		{
			name: "Authorization header always obfuscated",
			requestHeaders: map[string]string{
				"Authorization": "Bearer secret-token",
			},
			opts:             []middleware.LoggingOption{},
			wantHeaderMasked: "Authorization",
		},
		{
			name: "custom header obfuscated",
			requestHeaders: map[string]string{
				"X-Api-Key": "my-key",
			},
			opts:             []middleware.LoggingOption{middleware.WithObfuscatedHeaders("X-Api-Key")},
			wantHeaderMasked: "X-Api-Key",
		},
		{
			name:           "JSON body field obfuscated",
			requestBody:    `{"email":"user@example.com","password":"secret123"}`,
			opts:           []middleware.LoggingOption{middleware.WithRequestBody(true), middleware.WithObfuscatedBodyFields("password")},
			wantBodyMasked: "password",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			logger := newTestLogger(&buf)

			handler := middleware.Chain(okHandler(), middleware.Logging(logger, tc.opts...))

			var body io.Reader
			if tc.requestBody != "" {
				body = strings.NewReader(tc.requestBody)
			}

			req := httptest.NewRequest(http.MethodPost, "/test", body)
			for k, v := range tc.requestHeaders {
				req.Header.Set(k, v)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			incoming := decodeAllLogs(t, &buf)[0]

			if tc.wantHeaderMasked != "" {
				headers, ok := incoming["headers"].(map[string]any)
				if !ok {
					t.Fatal("expected headers field in log")
				}

				if headers[tc.wantHeaderMasked] != "***" {
					t.Errorf("header %q: expected ***, got %v", tc.wantHeaderMasked, headers[tc.wantHeaderMasked])
				}
			}

			if tc.wantBodyMasked != "" {
				bodyStr, ok := incoming["body"].(string)
				if !ok {
					t.Fatal("expected body field in log")
				}

				var bodyMap map[string]any
				if err := json.Unmarshal([]byte(bodyStr), &bodyMap); err != nil {
					t.Fatalf("body is not valid JSON: %v", err)
				}

				if bodyMap[tc.wantBodyMasked] != "***" {
					t.Errorf("body field %q: expected ***, got %v", tc.wantBodyMasked, bodyMap[tc.wantBodyMasked])
				}
			}
		})
	}
}

func TestLogging_body(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		opts         []middleware.LoggingOption
		requestBody  string
		responseBody string
		wantReqBody  bool
		wantRespBody bool
	}{
		{
			name:        "body not logged by default",
			requestBody: `{"key":"value"}`,
			wantReqBody: false,
		},
		{
			name:        "request body logged when enabled",
			opts:        []middleware.LoggingOption{middleware.WithRequestBody(true)},
			requestBody: `{"key":"value"}`,
			wantReqBody: true,
		},
		{
			name:         "response body logged when enabled",
			opts:         []middleware.LoggingOption{middleware.WithResponseBody(true)},
			responseBody: `{"id":"123"}`,
			wantRespBody: true,
		},
		{
			name:        "body truncated at max size",
			opts:        []middleware.LoggingOption{middleware.WithRequestBody(true), middleware.WithMaxBodySize(5)},
			requestBody: "hello world",
			wantReqBody: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			logger := newTestLogger(&buf)

			var h http.Handler
			if tc.responseBody != "" {
				h = jsonHandler(tc.responseBody)
			} else {
				h = okHandler()
			}

			handler := middleware.Chain(h, middleware.Logging(logger, tc.opts...))

			var body io.Reader
			if tc.requestBody != "" {
				body = strings.NewReader(tc.requestBody)
			}

			req := httptest.NewRequest(http.MethodPost, "/test", body)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			logs := decodeAllLogs(t, &buf)
			incomingLog := logs[0]
			outgoingLog := logs[1]

			_, hasReqBody := incomingLog["body"]
			if tc.wantReqBody && !hasReqBody {
				t.Error("expected body in incoming log, got none")
			}

			if !tc.wantReqBody && hasReqBody {
				t.Error("unexpected body in incoming log")
			}

			_, hasRespBody := outgoingLog["body"]
			if tc.wantRespBody && !hasRespBody {
				t.Error("expected body in outgoing log, got none")
			}
		})
	}
}
