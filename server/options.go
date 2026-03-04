// Package server provides HTTP server lifecycle management with graceful shutdown.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

const (
	defaultAddr              = ":8080"
	defaultShutdownTimeout   = 30 * time.Second
	defaultReadHeaderTimeout = 10 * time.Second
)

// options holds the configuration for a Server.
type options struct {
	addr              string
	handler           http.Handler
	logger            *slog.Logger
	shutdownTimeout   time.Duration
	readHeaderTimeout time.Duration
	baseCtx           context.Context //nolint:containedctx // intentional: base context for server lifetime
	healthChecks      *HealthChecks
}

// defaultOptions returns the default server options.
func defaultOptions() options {
	return options{
		addr:              defaultAddr,
		handler:           http.DefaultServeMux,
		logger:            slog.Default(),
		shutdownTimeout:   defaultShutdownTimeout,
		readHeaderTimeout: defaultReadHeaderTimeout,
		baseCtx:           context.Background(),
	}
}

// Option is a functional option for configuring a Server.
type Option func(*options)

// WithAddr sets the TCP address the server listens on (e.g. ":8080").
func WithAddr(addr string) Option {
	return func(o *options) {
		o.addr = addr
	}
}

// WithHandler sets the HTTP handler to serve requests.
// Typically a router with middlewares already applied.
func WithHandler(handler http.Handler) Option {
	return func(o *options) {
		o.handler = handler
	}
}

// WithLogger sets the structured logger used for server lifecycle events.
// It does not affect request-level logging, which is handled by middleware.
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		o.logger = logger
	}
}

// WithReadHeaderTimeout sets the maximum duration to read the request headers.
// This protects against Slowloris attacks. Defaults to 10 seconds.
func WithReadHeaderTimeout(d time.Duration) Option {
	return func(o *options) {
		o.readHeaderTimeout = d
	}
}

// WithShutdownTimeout sets the maximum duration to wait for in-flight requests
// and tracked goroutines to finish before forcing a shutdown.
// Defaults to 30 seconds.
func WithShutdownTimeout(d time.Duration) Option {
	return func(o *options) {
		o.shutdownTimeout = d
	}
}

// WithBaseContext sets the base context for the server and all derived contexts.
// The server's app context will be a child of this context.
// Defaults to context.Background().
func WithBaseContext(ctx context.Context) Option {
	return func(o *options) {
		o.baseCtx = ctx
	}
}
