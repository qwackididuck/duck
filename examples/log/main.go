// Example: structured logging with context propagation.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	ducklog "github.com/qwackididuck/duck/log"
)

func processRequest(ctx context.Context, logger *slog.Logger) {
	// Retrieve the logger enriched with context attributes.
	// Any attributes set via log.ContextWithAttrs are included automatically.
	log := ducklog.FromContext(ctx, logger)

	log.Info("processing request", slog.String("step", "start"))
	log.Debug("detail", slog.Int("items", 42))
	log.Warn("slow query", slog.Int64("duration_ms", 850))
	log.Info("processing request", slog.String("step", "done"))
}

func handler(logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Enrich the context with request-scoped attributes.
		// All logs using this context will include these fields.
		ctx := ducklog.ContextWithAttrs(r.Context(),
			slog.String("request_id", r.Header.Get("X-Request-Id")),
			slog.String("user_id", "usr_123"),
			slog.String("path", r.URL.Path),
		)

		processRequest(ctx, logger)

		w.WriteHeader(http.StatusOK)
	}
}

func main() {
	// JSON logger — recommended for production
	jsonLogger := ducklog.New(
		ducklog.WithFormat(ducklog.FormatJSON),
		ducklog.WithLevel(slog.LevelDebug),
		ducklog.WithOutput(os.Stdout),
	)

	// Text logger — readable in development
	textLogger := ducklog.New(
		ducklog.WithFormat(ducklog.FormatText),
		ducklog.WithLevel(slog.LevelDebug),
		ducklog.WithOutput(os.Stdout),
	)

	jsonLogger.Info("=== JSON format ===")

	ctx := ducklog.ContextWithAttrs(context.Background(),
		slog.String("service", "example"),
		slog.String("env", "dev"),
	)

	processRequest(ctx, jsonLogger)

	textLogger.Info("=== Text format ===")
	processRequest(ctx, textLogger)
}
