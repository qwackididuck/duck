// Example: HTTP client with retry, exponential backoff, and request/response logging.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"time"

	"github.com/qwackididuck/duck/httpclient"
	ducklog "github.com/qwackididuck/duck/log"
)

func main() {
	logger := ducklog.New(
		ducklog.WithFormat(ducklog.FormatText),
		ducklog.WithOutput(os.Stdout),
	)

	// ==========================================================================
	// 1. Basic client with retry and logging
	// ==========================================================================

	client := httpclient.New(
		httpclient.WithTimeout(30*time.Second),

		// Log all outgoing requests and incoming responses.
		// Authorization header is always obfuscated.
		httpclient.WithLogging(logger,
			httpclient.WithClientRequestBody(true),
			httpclient.WithClientResponseBody(true),
			httpclient.WithClientObfuscatedHeaders("Authorization"),
			httpclient.WithClientObfuscatedBodyFields("password"),
		),

		// Retry on transient errors with exponential backoff + jitter.
		// POST and other non-idempotent methods are only retried on network errors,
		// never on HTTP status codes.
		httpclient.WithRetry(
			[]httpclient.RetryCondition{
				httpclient.RetryOnStatusCodes(429, 502, 503, 504),
				httpclient.RetryOnNetworkErrors(),
			},
			httpclient.WithMaxAttempts(3),
			httpclient.WithExponentialBackoff(100*time.Millisecond),
		),
	)

	// ==========================================================================
	// 2. Demonstrate retry — server fails twice then succeeds
	// ==========================================================================

	fmt.Println("\n=== Retry demo ===")

	var attempts atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			logger.Info("server returning 503", slog.Int64("attempt", n))
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/api/data", http.NoBody)
	req.Header.Set("Authorization", "Bearer super-secret-token")

	resp, err := client.Do(req) //nolint:gosec // G704: URL from httptest.NewServer is localhost
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("final status: %d, body: %s, total attempts: %d\n",
		resp.StatusCode, body, attempts.Load())

	// ==========================================================================
	// 3. Custom retry condition
	// ==========================================================================

	fmt.Println("\n=== Custom retry condition ===")

	clientWithCustomRetry := httpclient.New(
		httpclient.WithTimeout(10*time.Second),
		httpclient.WithRetry(
			[]httpclient.RetryCondition{
				// Retry on any 5xx status code
				httpclient.RetryIf(func(_ *http.Request, resp *http.Response, err error) bool {
					if err != nil {
						return true
					}

					return resp != nil && resp.StatusCode >= 500
				}),
			},
			httpclient.WithMaxAttempts(4),
			httpclient.WithBackoff(httpclient.ConstantBackoff(50*time.Millisecond)),
		),
	)

	var customAttempts atomic.Int32

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := customAttempts.Add(1)
		if n < 4 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv2.Close()

	req2, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv2.URL, http.NoBody)

	resp2, err := clientWithCustomRetry.Do(req2) //nolint:gosec // G704: URL from httptest.NewServer is localhost
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		os.Exit(1)
	}
	defer resp2.Body.Close()

	fmt.Printf("succeeded on attempt %d with status %d\n", customAttempts.Load(), resp2.StatusCode)

	// ==========================================================================
	// 4. Request ID propagation
	// ==========================================================================

	fmt.Println("\n=== Request ID propagation ===")

	var receivedID string

	srv3 := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		receivedID = r.Header.Get("X-Request-Id")
	}))
	defer srv3.Close()

	clientWithLogging := httpclient.New(
		httpclient.WithLogging(logger),
	)

	req3, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv3.URL, http.NoBody)
	req3.Header.Set("X-Request-Id", "req-abc-123")

	resp3, _ := clientWithLogging.Do(req3) //nolint:gosec // G704: URL from httptest.NewServer is localhost
	if resp3 != nil {
		resp3.Body.Close()
	}

	fmt.Printf("downstream received X-Request-Id: %s\n", receivedID)
}
