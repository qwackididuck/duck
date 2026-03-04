package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// Server manages the lifecycle of an HTTP server with graceful shutdown.
// It tracks background goroutines and ensures they are given time to finish
// before the process exits.
type Server struct {
	httpServer    *http.Server
	opts          options
	appCtx        context.Context //nolint:containedctx // intentional: application context for background goroutines
	cancelApp     context.CancelFunc
	wg            sync.WaitGroup
	quit          chan struct{}
	healthMounted bool
}

// NewServer creates a new Server with the provided options.
// The server is not started until [Server.Start] is called.
func NewServer(opts ...Option) (*Server, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}

	if err := validate(&o); err != nil {
		return nil, fmt.Errorf("invalid server options: %w", err)
	}

	appCtx, cancelApp := context.WithCancel(o.baseCtx)

	return &Server{
		httpServer: &http.Server{
			Addr:              o.addr,
			Handler:           o.handler,
			ReadHeaderTimeout: o.readHeaderTimeout,
			BaseContext: func(_ net.Listener) context.Context {
				return appCtx
			},
		},
		opts:      o,
		appCtx:    appCtx,
		cancelApp: cancelApp,
		quit:      make(chan struct{}),
	}, nil
}

// ServeHTTP implements http.Handler — useful for testing without starting a real listener.
// It applies health check routes if configured.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mountHealthChecks()
	s.httpServer.Handler.ServeHTTP(w, r)
}

// Addr returns the TCP address the server is configured to listen on.
func (s *Server) Addr() string {
	return s.opts.addr
}

// Context returns the application context.
// It is canceled when a shutdown signal is received, allowing background
// goroutines to detect the shutdown and exit gracefully.
func (s *Server) Context() context.Context {
	return s.appCtx
}

// Go runs fn in a new goroutine tracked by the server's internal WaitGroup.
// The server will wait for all goroutines started with Go to finish during
// shutdown, up to the configured shutdown timeout.
//
// The provided context is the application context and will be canceled
// when a shutdown signal is received.
func (s *Server) Go(fn func(ctx context.Context)) {
	s.wg.Go(func() {
		fn(s.appCtx)
	})
}

// Start begins listening for HTTP requests and blocks until the server shuts down.
func (s *Server) Start() error {
	s.mountHealthChecks()

	sigCtx, stopSig := signal.NotifyContext(s.opts.baseCtx, os.Interrupt, syscall.SIGTERM)
	defer stopSig()

	serverErr := make(chan error, 1)

	s.opts.logger.Info("server starting", "addr", s.opts.addr)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- fmt.Errorf("server listen error: %w", err)
		}

		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		// Server failed to start or crashed before receiving a signal.
		s.cancelApp()

		return err

	case <-sigCtx.Done():
		s.opts.logger.Info("shutdown signal received, starting graceful shutdown")

	case <-s.quit:
		s.opts.logger.Info("programmatic shutdown requested")
	}

	return s.shutdown()
}

// Shutdown triggers a graceful shutdown programmatically.
// It is safe to call from any goroutine and is idempotent.
// This is useful in tests or when the caller manages shutdown logic externally.
func (s *Server) Shutdown() {
	select {
	case <-s.quit:
		// Already closed, nothing to do.
	default:
		close(s.quit)
	}
}

// mountHealthChecks registers /health and /ready on the server handler
// if health checks are configured. It is idempotent — safe to call multiple times.
func (s *Server) mountHealthChecks() {
	hc := s.opts.healthChecks
	if hc == nil || s.healthMounted {
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", hc.healthHandler())
	mux.HandleFunc("/ready", hc.readyHandler())
	mux.Handle("/", s.httpServer.Handler)
	s.httpServer.Handler = mux
	s.healthMounted = true
}

// shutdown performs the graceful shutdown sequence.
func (s *Server) shutdown() error {
	// Step 1: cancel the app context so background goroutines can stop.
	s.cancelApp()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.opts.shutdownTimeout)
	defer cancel()

	// Step 2: stop accepting new requests and wait for in-flight ones.
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		s.opts.logger.Error("http server shutdown error", "error", err)

		return fmt.Errorf("http server shutdown: %w", err)
	}

	s.opts.logger.Info("http server stopped, waiting for background goroutines")

	// Step 3: wait for tracked background goroutines.
	goroutinesDone := make(chan struct{})

	go func() {
		s.wg.Wait()
		close(goroutinesDone)
	}()

	select {
	case <-goroutinesDone:
		s.opts.logger.Info("shutdown complete")
	case <-shutdownCtx.Done():
		s.opts.logger.Warn("shutdown timeout exceeded, forcing shutdown")

		return fmt.Errorf("shutdown timeout exceeded: %w", context.DeadlineExceeded)
	}

	return nil
}

// validate checks that the required options are set.
func validate(o *options) error {
	if o.addr == "" {
		return errors.New("addr must not be empty")
	}

	if o.handler == nil {
		return errors.New("handler must not be nil")
	}

	if o.logger == nil {
		return errors.New("logger must not be nil")
	}

	if o.shutdownTimeout <= 0 {
		return errors.New("shutdown timeout must be positive")
	}

	return nil
}
