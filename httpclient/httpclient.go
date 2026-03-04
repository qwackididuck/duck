// Package httpclient provides a factory for building *http.Client instances
// with composable transports for retry, logging, and tracing.
//
// Each feature is implemented as an http.RoundTripper that wraps the next
// transport in the chain — the standard Go pattern for HTTP client middleware.
//
// Usage:
//
//	client := httpclient.New(
//	    httpclient.WithTimeout(30 * time.Second),
//	    httpclient.WithLogging(logger),
//	    httpclient.WithRetry(
//	        httpclient.RetryOnStatusCodes(429, 502, 503),
//	        httpclient.WithMaxAttempts(3),
//	        httpclient.WithExponentialBackoff(100 * time.Millisecond),
//	    ),
//	)
//
//	resp, err := client.Do(req)
package httpclient

import (
	"net/http"
	"time"
)

// clientOptions holds the configuration for building an *http.Client.
type clientOptions struct {
	timeout   time.Duration
	transport http.RoundTripper
	retry     *retryOptions
	logging   *loggingTransportOptions
}

// Option is a functional option for [New].
type Option func(*clientOptions)

// WithTimeout sets the HTTP client timeout for the entire request/response
// cycle, including retries. Defaults to no timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *clientOptions) {
		o.timeout = d
	}
}

// WithTransport sets the base RoundTripper. Defaults to http.DefaultTransport.
// All other transports (retry, logging) wrap this base transport.
func WithTransport(t http.RoundTripper) Option {
	return func(o *clientOptions) {
		o.transport = t
	}
}

// New builds and returns a *http.Client with the configured transports.
//
// Transport chain (outermost to innermost):
//
//	logging → retry → base transport
//
// Logging wraps retry so that each attempt is visible in the logs.
func New(opts ...Option) *http.Client {
	o := &clientOptions{
		transport: http.DefaultTransport,
	}

	for _, opt := range opts {
		opt(o)
	}

	transport := o.transport

	if o.retry != nil {
		transport = &retryTransport{
			next: transport,
			opts: o.retry,
		}
	}

	if o.logging != nil {
		transport = &loggingTransport{
			next: transport,
			opts: o.logging,
		}
	}

	return &http.Client{
		Timeout:   o.timeout,
		Transport: transport,
	}
}
