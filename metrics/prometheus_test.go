package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/qwackididuck/duck/metrics"
)

func TestNewPrometheus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
		opts      []metrics.PrometheusOption
		wantErr   bool
	}{
		{
			name:      "default options",
			namespace: "myapp",
		},
		{
			name:      "with const labels",
			namespace: "myapp",
			opts: []metrics.PrometheusOption{
				metrics.WithConstLabels(prometheus.Labels{"env": "test"}),
			},
		},
		{
			name:      "with additional labels",
			namespace: "myapp",
			opts: []metrics.PrometheusOption{
				metrics.WithAdditionalLabels("tenant"),
			},
		},
		{
			name:      "with custom buckets",
			namespace: "myapp",
			opts: []metrics.PrometheusOption{
				metrics.WithBuckets([]float64{.01, .1, 1, 10}),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p, err := metrics.NewPrometheus(tc.namespace, tc.opts...)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("NewPrometheus() unexpected error: %v", err)
			}

			if p == nil {
				t.Fatal("expected non-nil Prometheus")
			}
		})
	}
}

func TestPrometheus_NotifyServerExchange(t *testing.T) {
	t.Parallel()

	p, err := metrics.NewPrometheus("test")
	if err != nil {
		t.Fatalf("NewPrometheus: %v", err)
	}

	tests := []struct {
		name       string
		statusCode int
		route      string
		method     string
		duration   time.Duration
	}{
		{name: "GET 200", statusCode: 200, route: "/users", method: "GET", duration: 10 * time.Millisecond},
		{name: "POST 201", statusCode: 201, route: "/users", method: "POST", duration: 50 * time.Millisecond},
		{name: "GET 404", statusCode: 404, route: "/missing", method: "GET", duration: 1 * time.Millisecond},
		{name: "GET 500", statusCode: 500, route: "/error", method: "GET", duration: 100 * time.Millisecond},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// NotifyServerExchange must not panic — that is the primary invariant.
			p.NotifyServerExchange(tc.statusCode, tc.route, tc.method, tc.duration, nil)
		})
	}
}

func TestPrometheus_Handler(t *testing.T) {
	t.Parallel()

	p, err := metrics.NewPrometheus("testapp")
	if err != nil {
		t.Fatalf("NewPrometheus: %v", err)
	}

	p.NotifyServerExchange(200, "/users", "GET", 10*time.Millisecond, nil)
	p.NotifyServerExchange(500, "/users", "POST", 100*time.Millisecond, nil)

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("handler status: expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()

	for _, want := range []string{
		"testapp_http_requests_total",
		"testapp_http_request_duration_seconds",
		`route="/users"`,
		`method="GET"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestPrometheus_additionalLabels(t *testing.T) {
	t.Parallel()

	p, err := metrics.NewPrometheus("testapp",
		metrics.WithAdditionalLabels("tenant"),
	)
	if err != nil {
		t.Fatalf("NewPrometheus: %v", err)
	}

	p.NotifyServerExchange(200, "/api", "GET", 5*time.Millisecond,
		map[string]string{"tenant": "acme"},
	)

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), `tenant="acme"`) {
		t.Error("expected tenant label in metrics output")
	}
}
