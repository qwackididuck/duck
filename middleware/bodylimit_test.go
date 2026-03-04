package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qwackididuck/duck/middleware"
)

// echoBodyHandler reads and echoes the request body so tests can verify
// it was passed through correctly.
func echoBodyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)

			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
}

func TestBodyLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		maxBytes       int64
		body           string
		contentLength  int // -1 to not set
		wantStatus     int
		wantBodyEchoed bool
	}{
		{
			name:           "body within limit passes through",
			maxBytes:       100,
			body:           "hello",
			contentLength:  -1,
			wantStatus:     http.StatusOK,
			wantBodyEchoed: true,
		},
		{
			name:          "content-length over limit rejected immediately",
			maxBytes:      10,
			body:          "hello",
			contentLength: 100,
			wantStatus:    http.StatusRequestEntityTooLarge,
		},
		{
			name:           "body exactly at limit passes through",
			maxBytes:       5,
			body:           "hello",
			contentLength:  -1,
			wantStatus:     http.StatusOK,
			wantBodyEchoed: true,
		},
		{
			name:          "body over limit rejected during read",
			maxBytes:      4,
			body:          "hello world",
			contentLength: -1,
			wantStatus:    http.StatusRequestEntityTooLarge,
		},
		{
			name:           "zero maxBytes uses default limit (1MB) — small body passes",
			maxBytes:       0,
			body:           "small",
			contentLength:  -1,
			wantStatus:     http.StatusOK,
			wantBodyEchoed: true,
		},
		{
			name:           "negative maxBytes disables limit",
			maxBytes:       -1,
			body:           strings.Repeat("x", 10*1024*1024), // 10MB
			contentLength:  -1,
			wantStatus:     http.StatusOK,
			wantBodyEchoed: true,
		},
		{
			name:           "empty body always passes",
			maxBytes:       10,
			body:           "",
			contentLength:  -1,
			wantStatus:     http.StatusOK,
			wantBodyEchoed: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := middleware.Chain(echoBodyHandler(), middleware.BodyLimit(tc.maxBytes))

			var reqBody io.Reader
			if tc.body != "" {
				reqBody = strings.NewReader(tc.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/", reqBody)

			if tc.contentLength >= 0 {
				req.ContentLength = int64(tc.contentLength)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status: expected %d, got %d", tc.wantStatus, rec.Code)
			}

			if tc.wantBodyEchoed {
				got := rec.Body.String()
				if got != tc.body {
					t.Errorf("body: expected %q, got %q", tc.body, got)
				}
			}
		})
	}
}
