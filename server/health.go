package server

import (
	"context"
	"encoding/json"
	"net/http"
)

// Status represents the health status of a service or dependency.
type Status string

const (
	// StatusOK indicates the service is healthy and operational.
	StatusOK Status = "OK"
	// StatusKO indicates the service is unhealthy or unreachable.
	StatusKO Status = "KO"
)

// ServiceStatus is the result returned by a [Stater] implementation.
type ServiceStatus struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
}

// Stater is the interface that dependencies must implement to participate
// in the /ready health check.
//
// The implementation is responsible for managing its own timeout — duck
// calls Status() as-is and trusts the implementation to return promptly.
//
//	type PostgresChecker struct { db *sql.DB }
//
//	func (p *PostgresChecker) Status(ctx context.Context) server.ServiceStatus {
//	    ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
//	    defer cancel()
//	    if err := p.db.PingContext(ctx); err != nil {
//	        return server.ServiceStatus{Name: "postgres", Status: server.StatusKO}
//	    }
//	    return server.ServiceStatus{Name: "postgres", Status: server.StatusOK}
//	}
type Stater interface {
	Status(ctx context.Context) ServiceStatus
}

// healthResponse is the JSON body returned by /health.
type healthResponse struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
}

// readyResponse is the JSON body returned by /ready.
type readyResponse struct {
	Name     string          `json:"name"`
	Status   Status          `json:"status"`
	Services []ServiceStatus `json:"services"`
}

// HealthChecks holds the configuration for the health check endpoints.
type HealthChecks struct {
	serviceName string
	staters     []Stater
	koStatus    int
}

// HealthCheckOption is a functional option for [WithHealthChecks].
type HealthCheckOption func(*HealthChecks)

// WithKOStatus sets the HTTP status code returned when the service or one of
// its dependencies is KO. Defaults to 200.
//
// Use WithKOStatus(http.StatusServiceUnavailable) to return 503 — recommended
// for Kubernetes liveness/readiness probes to automatically remove the pod
// from the load balancer pool.
func WithKOStatus(code int) HealthCheckOption {
	return func(h *HealthChecks) {
		h.koStatus = code
	}
}

// WithHealthChecks enables the /health and /ready endpoints on the server.
//
//	srv, _ := server.New(
//	    server.WithHealthChecks("my-service",
//	        server.WithKOStatus(http.StatusServiceUnavailable),
//	    )
//	)
//
// Register dependencies for the /ready endpoint via [WithDependency].
func WithHealthChecks(serviceName string, opts ...HealthCheckOption) Option {
	return func(o *options) {
		hc := o.healthChecks
		if hc == nil {
			hc = &HealthChecks{koStatus: http.StatusOK}
		}

		hc.serviceName = serviceName

		for _, opt := range opts {
			opt(hc)
		}

		o.healthChecks = hc
	}
}

// WithDependency registers a [Stater] to be checked by the /ready endpoint.
// Call this multiple times to register multiple dependencies.
//
//	server.WithDependency(&PostgresChecker{db: db})
//	server.WithDependency(&RedisChecker{client: rdb})
func WithDependency(stater Stater) Option {
	return func(o *options) {
		if stater == nil {
			return
		}

		if o.healthChecks == nil {
			o.healthChecks = &HealthChecks{koStatus: http.StatusOK}
		}

		o.healthChecks.staters = append(o.healthChecks.staters, stater)
	}
}

// healthHandler returns the /health handler — liveness probe.
// It always returns OK as long as the server is running.
func (hc *HealthChecks) healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(healthResponse{
			Name:   hc.serviceName,
			Status: StatusOK,
		})
	}
}

// readyHandler returns the /ready handler — readiness probe.
// It checks all registered dependencies sequentially and returns KO if any fail.
func (hc *HealthChecks) readyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		services := make([]ServiceStatus, 0, len(hc.staters))
		overallStatus := StatusOK

		for _, stater := range hc.staters {
			svc := stater.Status(r.Context())
			services = append(services, svc)

			if svc.Status == StatusKO {
				overallStatus = StatusKO
			}
		}

		statusCode := http.StatusOK
		if overallStatus == StatusKO {
			statusCode = hc.koStatus
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(readyResponse{
			Name:     hc.serviceName,
			Status:   overallStatus,
			Services: services,
		})
	}
}
