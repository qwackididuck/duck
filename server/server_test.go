package server_test

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qwackididuck/duck/server"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func freeAddr(t *testing.T) string {
	t.Helper()

	lc := &net.ListenConfig{}

	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freeAddr: %v", err)
	}

	addr := ln.Addr().String()

	if err := ln.Close(); err != nil {
		t.Fatalf("freeAddr close: %v", err)
	}

	return addr
}

func waitForServer(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	dialer := &net.Dialer{}

	for time.Now().Before(deadline) {
		conn, err := dialer.DialContext(context.Background(), "tcp", addr)
		if err == nil {
			_ = conn.Close()

			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("server at %s did not become ready within timeout", addr)
}

// --- New() validation ---

func TestNew_validationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []server.Option
	}{
		{
			name: "empty addr",
			opts: []server.Option{
				server.WithAddr(""),
				server.WithHandler(http.NewServeMux()),
				server.WithLogger(newTestLogger()),
			},
		},
		{
			name: "nil handler",
			opts: []server.Option{
				server.WithAddr(":8080"),
				server.WithHandler(nil),
				server.WithLogger(newTestLogger()),
			},
		},
		{
			name: "nil logger",
			opts: []server.Option{
				server.WithAddr(":8080"),
				server.WithHandler(http.NewServeMux()),
				server.WithLogger(nil),
			},
		},
		{
			name: "zero shutdown timeout",
			opts: []server.Option{
				server.WithAddr(":8080"),
				server.WithHandler(http.NewServeMux()),
				server.WithLogger(newTestLogger()),
				server.WithShutdownTimeout(0),
			},
		},
		{
			name: "negative shutdown timeout",
			opts: []server.Option{
				server.WithAddr(":8080"),
				server.WithHandler(http.NewServeMux()),
				server.WithLogger(newTestLogger()),
				server.WithShutdownTimeout(-1 * time.Second),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := server.NewServer(tc.opts...)
			if err == nil {
				t.Fatalf("New() expected error for %q, got nil", tc.name)
			}
		})
	}
}

func TestNew_defaults(t *testing.T) {
	t.Parallel()

	srv, err := server.NewServer(
		server.WithAddr(freeAddr(t)),
		server.WithHandler(http.NewServeMux()),
		server.WithLogger(newTestLogger()),
	)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	if srv == nil {
		t.Fatal("New() returned nil server")
	}
}

// --- Lifecycle ---

func TestServer_context_cancelledOnShutdown(t *testing.T) {
	t.Parallel()

	srv, err := server.NewServer(
		server.WithAddr(freeAddr(t)),
		server.WithHandler(http.NewServeMux()),
		server.WithLogger(newTestLogger()),
		server.WithShutdownTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	ctx := srv.Context()
	startErr := make(chan error, 1)

	go func() { startErr <- srv.Start() }()

	waitForServer(t, srv.Addr())
	srv.Shutdown()

	select {
	case <-ctx.Done():
		// Expected: context was canceled.
	case <-time.After(5 * time.Second):
		t.Fatal("app context was not canceled after shutdown")
	}

	if err := <-startErr; err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}
}

// --- Go() goroutine tracking ---

func TestServer_Go_tracksGoroutines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sleepTime time.Duration
	}{
		{name: "fast goroutine", sleepTime: 10 * time.Millisecond},
		{name: "slow goroutine", sleepTime: 100 * time.Millisecond},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv, err := server.NewServer(
				server.WithAddr(freeAddr(t)),
				server.WithHandler(http.NewServeMux()),
				server.WithLogger(newTestLogger()),
				server.WithShutdownTimeout(5*time.Second),
			)
			if err != nil {
				t.Fatalf("New(): %v", err)
			}

			var ran atomic.Bool

			srv.Go(func(ctx context.Context) {
				<-ctx.Done()
				time.Sleep(tc.sleepTime)
				ran.Store(true)
			})

			startErr := make(chan error, 1)

			go func() { startErr <- srv.Start() }()

			waitForServer(t, srv.Addr())
			srv.Shutdown()

			if err := <-startErr; err != nil {
				t.Fatalf("Start() unexpected error: %v", err)
			}

			if !ran.Load() {
				t.Fatal("background goroutine did not run to completion before shutdown")
			}
		})
	}
}

// --- HTTP serving ---

func TestServer_serveHTTP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		path           string
		method         string
		expectedStatus int
	}{
		{name: "GET /hello returns 200", path: "/hello", method: http.MethodGet, expectedStatus: http.StatusOK},
		{name: "GET /missing returns 404", path: "/missing", method: http.MethodGet, expectedStatus: http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc("GET /hello", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			srv, err := server.NewServer(
				server.WithAddr(freeAddr(t)),
				server.WithHandler(mux),
				server.WithLogger(newTestLogger()),
			)
			if err != nil {
				t.Fatalf("New(): %v", err)
			}

			startErr := make(chan error, 1)

			go func() { startErr <- srv.Start() }()

			waitForServer(t, srv.Addr())

			req, err := http.NewRequestWithContext(
				context.Background(), tc.method,
				"http://"+srv.Addr()+tc.path, http.NoBody,
			)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}

			resp, err := http.DefaultClient.Do(req) //nolint:bodyclose,gosec // bodyclose: closed below — gosec G704: URL from httptest.NewServer is always localhost
			if err != nil {
				t.Fatalf("%s %s: %v", tc.method, tc.path, err)
			}

			defer resp.Body.Close()

			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, resp.StatusCode)
			}

			srv.Shutdown()

			if err := <-startErr; err != nil {
				t.Fatalf("Start() unexpected error: %v", err)
			}
		})
	}
}
