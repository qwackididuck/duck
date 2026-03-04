package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qwackididuck/duck/middleware"
)

// mockMetricsProvider captures calls to NotifyServerExchange for assertions.
type mockMetricsProvider struct {
	calls []metricsCall
}

type metricsCall struct {
	statusCode       int
	route            string
	method           string
	duration         time.Duration
	additionalLabels map[string]string
}

func (m *mockMetricsProvider) NotifyServerExchange(
	statusCode int,
	route, method string,
	duration time.Duration,
	additionalLabels map[string]string,
) {
	m.calls = append(m.calls, metricsCall{
		statusCode:       statusCode,
		route:            route,
		method:           method,
		duration:         duration,
		additionalLabels: additionalLabels,
	})
}

func TestHTTPMetrics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		method         string
		path           string
		handler        http.Handler
		opts           []middleware.HTTPMetricsOption
		wantStatus     int
		wantRoute      string
		wantMethod     string
		wantAddlLabels map[string]string
	}{
		{
			name:       "records status, route and method",
			method:     http.MethodGet,
			path:       "/users",
			handler:    okHandler(),
			wantStatus: http.StatusOK,
			wantRoute:  "/users",
			wantMethod: http.MethodGet,
		},
		{
			name:    "uses path cleaner for route label",
			method:  http.MethodGet,
			path:    "/users/123",
			handler: okHandler(),
			opts: []middleware.HTTPMetricsOption{
				middleware.WithPathCleaner(func(_ *http.Request) string {
					return "/users/{id}"
				}),
			},
			wantStatus: http.StatusOK,
			wantRoute:  "/users/{id}",
			wantMethod: http.MethodGet,
		},
		{
			name:   "restricts unknown method to OTHER",
			method: "BREW",
			path:   "/coffee",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			}),
			wantStatus: http.StatusTeapot,
			wantRoute:  "/coffee",
			wantMethod: "OTHER",
		},
		{
			name:       "implicit 200 recorded when WriteHeader not called",
			method:     http.MethodGet,
			path:       "/silent",
			handler:    http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}),
			wantStatus: http.StatusOK,
			wantRoute:  "/silent",
			wantMethod: http.MethodGet,
		},
		{
			name:    "additional labels from request forwarded",
			method:  http.MethodPost,
			path:    "/api",
			handler: okHandler(),
			opts: []middleware.HTTPMetricsOption{
				middleware.WithLabelsFromRequest(func(_ *http.Request) map[string]string {
					return map[string]string{"tenant": "acme"}
				}),
			},
			wantStatus:     http.StatusOK,
			wantRoute:      "/api",
			wantMethod:     http.MethodPost,
			wantAddlLabels: map[string]string{"tenant": "acme"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mock := &mockMetricsProvider{}
			handler := middleware.Chain(tc.handler, middleware.HTTPMetrics(mock, tc.opts...))

			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if len(mock.calls) != 1 {
				t.Fatalf("expected 1 metrics call, got %d", len(mock.calls))
			}

			call := mock.calls[0]

			if call.statusCode != tc.wantStatus {
				t.Errorf("statusCode: expected %d, got %d", tc.wantStatus, call.statusCode)
			}

			if call.route != tc.wantRoute {
				t.Errorf("route: expected %q, got %q", tc.wantRoute, call.route)
			}

			if call.method != tc.wantMethod {
				t.Errorf("method: expected %q, got %q", tc.wantMethod, call.method)
			}

			if call.duration <= 0 {
				t.Error("duration: expected positive duration")
			}

			for k, want := range tc.wantAddlLabels {
				if got := call.additionalLabels[k]; got != want {
					t.Errorf("additionalLabels[%q]: expected %q, got %q", k, want, got)
				}
			}
		})
	}
}

func TestStatusLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code int
		want string
	}{
		{200, "2xx"},
		{201, "2xx"},
		{301, "3xx"},
		{404, "4xx"},
		{500, "5xx"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()

			if got := middleware.StatusLabel(tc.code); got != tc.want {
				t.Errorf("StatusLabel(%d): expected %q, got %q", tc.code, tc.want, got)
			}
		})
	}
}
