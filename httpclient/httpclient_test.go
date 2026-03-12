package httpclient_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qwackididuck/duck/httpclient"
	ducklog "github.com/qwackididuck/duck/log"
)

// --- helpers ---

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return ducklog.New(
		ducklog.WithFormat(ducklog.FormatJSON),
		ducklog.WithOutput(buf),
	)
}

// --- TestNew ---

func TestNew_returnsStdClient(t *testing.T) {
	t.Parallel()

	client := httpclient.New()

	if client == nil {
		t.Fatal("expected non-nil *http.Client")
	}
}

func TestNew_withTimeout(t *testing.T) {
	t.Parallel()

	client := httpclient.New(httpclient.WithTimeout(5 * time.Second))

	if client.Timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", client.Timeout)
	}
}

// --- TestRetry ---

func TestRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		failStatus  int
		failCount   int
		maxAttempts int
		conditions  []httpclient.RetryCondition
		wantStatus  int
		wantCalls   int
	}{
		{
			name:        "succeeds on second attempt after 503",
			failStatus:  http.StatusServiceUnavailable,
			failCount:   1,
			maxAttempts: 3,
			conditions:  []httpclient.RetryCondition{httpclient.RetryOnStatusCodes(503)},
			wantStatus:  http.StatusOK,
			wantCalls:   2,
		},
		{
			name:        "exhausts all attempts and returns last response",
			failStatus:  http.StatusServiceUnavailable,
			failCount:   5,
			maxAttempts: 3,
			conditions:  []httpclient.RetryCondition{httpclient.RetryOnStatusCodes(503)},
			wantStatus:  http.StatusServiceUnavailable,
			wantCalls:   3,
		},
		{
			name:        "no retry when condition not met",
			failStatus:  http.StatusBadRequest,
			failCount:   1,
			maxAttempts: 3,
			conditions:  []httpclient.RetryCondition{httpclient.RetryOnStatusCodes(503)},
			wantStatus:  http.StatusBadRequest,
			wantCalls:   1,
		},
		{
			name:        "retries on 429 and 503",
			failStatus:  http.StatusTooManyRequests,
			failCount:   1,
			maxAttempts: 3,
			conditions:  []httpclient.RetryCondition{httpclient.RetryOnStatusCodes(429, 503)},
			wantStatus:  http.StatusOK,
			wantCalls:   2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n := int(calls.Add(1))
				if n <= tc.failCount {
					w.WriteHeader(tc.failStatus)

					return
				}

				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			client := httpclient.New(
				httpclient.WithRetry(
					tc.conditions,
					httpclient.WithMaxAttempts(tc.maxAttempts),
					httpclient.WithBackoff(httpclient.NoBackoff()),
				),
			)

			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, http.NoBody)

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status: expected %d, got %d", tc.wantStatus, resp.StatusCode)
			}

			if int(calls.Load()) != tc.wantCalls {
				t.Errorf("calls: expected %d, got %d", tc.wantCalls, calls.Load())
			}
		})
	}
}

func TestRetry_contextCancellation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := httpclient.New(
		httpclient.WithRetry(
			[]httpclient.RetryCondition{httpclient.RetryOnStatusCodes(503)},
			httpclient.WithMaxAttempts(10),
			httpclient.WithBackoff(httpclient.ConstantBackoff(50*time.Millisecond)),
		),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, http.NoBody)

	_, err := client.Do(req)
	if err == nil {
		t.Fatal("expected error from context cancellation, got nil")
	}
}

func TestRetry_nonIdempotentMethodNotRetried(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := httpclient.New(
		httpclient.WithRetry(
			[]httpclient.RetryCondition{httpclient.RetryOnStatusCodes(503)},
			httpclient.WithMaxAttempts(3),
			httpclient.WithBackoff(httpclient.NoBackoff()),
		),
	)

	body := strings.NewReader(`{"name":"alice"}`)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL, body)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// POST must not be retried on HTTP status errors — only 1 call expected.
	if calls.Load() != 1 {
		t.Errorf("POST should not be retried: expected 1 call, got %d", calls.Load())
	}
}

// --- TestLogging ---

func TestLogging_outgoingRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		opts           []httpclient.LoggingOption
		requestBody    string
		wantReqBody    bool
		wantObfuscated string
	}{
		{
			name:        "logs outgoing_request event",
			wantReqBody: false,
		},
		{
			name:        "logs request body when enabled",
			opts:        []httpclient.LoggingOption{httpclient.WithClientRequestBody(true)},
			requestBody: `{"key":"value"}`,
			wantReqBody: true,
		},
		{
			name: "obfuscates Authorization header",
			opts: []httpclient.LoggingOption{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			var buf bytes.Buffer

			logger := newTestLogger(&buf)

			client := httpclient.New(httpclient.WithLogging(logger, tc.opts...))

			var body io.Reader
			if tc.requestBody != "" {
				body = strings.NewReader(tc.requestBody)
			}

			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL, body)
			req.Header.Set("Authorization", "Bearer secret")

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer resp.Body.Close()

			logs := buf.String()

			if !strings.Contains(logs, "outgoing_request") {
				t.Error("expected outgoing_request event in logs")
			}

			if !strings.Contains(logs, "incoming_response") {
				t.Error("expected incoming_response event in logs")
			}

			if !strings.Contains(logs, `"***"`) {
				t.Error("expected Authorization header to be obfuscated")
			}

			if tc.wantReqBody && !strings.Contains(logs, "key") {
				t.Error("expected request body in logs")
			}
		})
	}
}

func TestLogging_requestIDPropagation(t *testing.T) {
	t.Parallel()

	var receivedID string

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		receivedID = r.Header.Get("X-Request-Id")
	}))
	defer srv.Close()

	var buf bytes.Buffer

	client := httpclient.New(httpclient.WithLogging(newTestLogger(&buf)))

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, http.NoBody)
	req.Header.Set("X-Request-Id", "test-request-id-123")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if receivedID != "test-request-id-123" {
		t.Errorf("X-Request-Id not propagated: got %q", receivedID)
	}
}
