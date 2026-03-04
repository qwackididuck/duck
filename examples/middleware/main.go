// Example: middleware chain with logging, body limit, and Prometheus metrics.
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"

	ducklog "github.com/qwackididuck/duck/log"
	"github.com/qwackididuck/duck/metrics"
	"github.com/qwackididuck/duck/middleware"
	"github.com/qwackididuck/duck/server"
)

func main() {
	logger := ducklog.New(
		ducklog.WithFormat(ducklog.FormatJSON),
		ducklog.WithOutput(os.Stdout),
	)

	// Prometheus metrics provider
	prom, err := metrics.NewPrometheus("myapp",
		metrics.WithAdditionalLabels("tenant"),
	)
	if err != nil {
		logger.Error("metrics setup", "err", err)
		os.Exit(1)
	}

	r := chi.NewRouter()

	// Middleware chain — applied in order (first = outermost)
	r.Use(middleware.HTTPMetrics(prom,
		middleware.WithPathCleaner(func(r *http.Request) string {
			// Use chi's route pattern to avoid high cardinality
			// e.g. /users/123 → /users/{id}
			return chi.RouteContext(r.Context()).RoutePattern()
		}),
		middleware.WithLabelsFromRequest(func(r *http.Request) map[string]string {
			return map[string]string{
				"tenant": r.Header.Get("X-Tenant-Id"),
			}
		}),
	),

		// Logging — logs request and response with body capture
		middleware.Logging(logger,
			middleware.WithRequestBody(true),
			middleware.WithResponseBody(true),
			middleware.WithMaxBodySize(4*1024), // 4KB max logged
			middleware.WithObfuscatedHeaders("Authorization", "Cookie"),
			middleware.WithObfuscatedBodyFields("password", "secret", "token"),
		),

		// Body limit — rejects requests over 1MB
		middleware.BodyLimit(1*1024*1024),
	)

	// Routes
	r.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "name": "Alice"})
	})

	r.Post("/users", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(body)
	})

	// Prometheus metrics endpoint
	r.Handle("/metrics", prom.Handler())

	srv, err := server.NewServer(
		server.WithAddr(":8080"),
		server.WithHandler(r),
		server.WithLogger(logger),
		server.WithShutdownTimeout(10*time.Second),
	)
	if err != nil {
		logger.Error("server setup", "err", err)
		os.Exit(1)
	}

	logger.Info("starting", slog.String("addr", ":8080"))

	if err := srv.Start(); err != nil {
		logger.Error("server", "err", err)
		os.Exit(1)
	}
}
