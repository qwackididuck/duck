package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qwackididuck/duck/server"
)

// --- Stater mocks ---

type okStater struct{ name string }

func (s *okStater) Status(_ context.Context) server.ServiceStatus {
	return server.ServiceStatus{Name: s.name, Status: server.StatusOK}
}

type koStater struct{ name string }

func (s *koStater) Status(_ context.Context) server.ServiceStatus {
	return server.ServiceStatus{Name: s.name, Status: server.StatusKO}
}

// --- /health ---

func TestHealthHandler(t *testing.T) {
	t.Parallel()

	srv, err := server.NewServer(
		server.WithAddr(":0"),
		server.WithHandler(http.NotFoundHandler()),
		server.WithHealthChecks("my-service"),
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Name != "my-service" {
		t.Errorf("name: expected %q, got %q", "my-service", resp.Name)
	}

	if resp.Status != string(server.StatusOK) {
		t.Errorf("status: expected OK, got %q", resp.Status)
	}
}

// --- /ready ---

func TestReadyHandler_allOK(t *testing.T) {
	t.Parallel()

	srv, err := server.NewServer(
		server.WithAddr(":0"),
		server.WithHandler(http.NotFoundHandler()),
		server.WithHealthChecks("my-service"),
		server.WithDependency(&okStater{"postgres"}),
		server.WithDependency(&okStater{"redis"}),
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Status   string `json:"status"`
		Services []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"services"`
	}

	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Status != string(server.StatusOK) {
		t.Errorf("expected overall OK, got %q", resp.Status)
	}

	if len(resp.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(resp.Services))
	}
}

func TestReadyHandler_oneKO_returns200ByDefault(t *testing.T) {
	t.Parallel()

	srv, err := server.NewServer(
		server.WithAddr(":0"),
		server.WithHandler(http.NotFoundHandler()),
		server.WithHealthChecks("my-service"),
		server.WithDependency(&okStater{"postgres"}),
		server.WithDependency(&koStater{"redis"}),
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (default KO status), got %d", rec.Code)
	}

	var resp struct {
		Status string `json:"status"`
	}

	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Status != string(server.StatusKO) {
		t.Errorf("expected overall KO, got %q", resp.Status)
	}
}

func TestReadyHandler_oneKO_with503(t *testing.T) {
	t.Parallel()

	srv, err := server.NewServer(
		server.WithAddr(":0"),
		server.WithHandler(http.NotFoundHandler()),
		server.WithHealthChecks("my-service",
			server.WithKOStatus(http.StatusServiceUnavailable),
		),
		server.WithDependency(&koStater{"postgres"}),
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestReadyHandler_noDependencies(t *testing.T) {
	t.Parallel()

	srv, err := server.NewServer(
		server.WithAddr(":0"),
		server.WithHandler(http.NotFoundHandler()),
		server.WithHealthChecks("my-service"),
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ready", http.NoBody)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Status   string `json:"status"`
		Services []any  `json:"services"`
	}

	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Status != string(server.StatusOK) {
		t.Errorf("expected OK, got %q", resp.Status)
	}

	if len(resp.Services) != 0 {
		t.Errorf("expected empty services, got %d", len(resp.Services))
	}
}

func TestNoHealthChecks_routesNotMounted(t *testing.T) {
	t.Parallel()

	srv, err := server.NewServer(
		server.WithAddr(":0"),
		server.WithHandler(http.NotFoundHandler()),
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	for _, path := range []string{"/health", "/ready"} {
		req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("path %s: expected 404 when health checks not configured, got %d", path, rec.Code)
		}
	}
}
