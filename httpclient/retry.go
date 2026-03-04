package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"time"
)

// RetryCondition is a function that decides whether a request should be
// retried based on the original request, the response, and the error from
// the previous attempt. req is always non-nil. resp is nil on network errors.
type RetryCondition func(req *http.Request, resp *http.Response, err error) bool

// retryOptions holds the retry configuration.
type retryOptions struct {
	maxAttempts int
	conditions  []RetryCondition
	backoff     BackoffStrategy
}

// RetryOption is a functional option for [WithRetry].
type RetryOption func(*retryOptions)

// WithMaxAttempts sets the maximum number of attempts (including the first).
// Defaults to 3.
func WithMaxAttempts(n int) RetryOption {
	return func(o *retryOptions) {
		if n < 1 {
			n = 1
		}

		o.maxAttempts = n
	}
}

// WithBackoff sets the backoff strategy between retry attempts.
// Defaults to [ExponentialBackoff] with a 100ms base.
func WithBackoff(s BackoffStrategy) RetryOption {
	return func(o *retryOptions) {
		o.backoff = s
	}
}

// WithExponentialBackoff is a convenience option that sets exponential backoff
// with the given base duration and full jitter.
func WithExponentialBackoff(base time.Duration) RetryOption {
	return WithBackoff(ExponentialBackoff(base))
}

// WithRetry configures retry behavior for the client.
// Conditions are OR-ed — a request is retried if any condition returns true.
func WithRetry(conditions []RetryCondition, opts ...RetryOption) Option {
	return func(o *clientOptions) {
		ro := &retryOptions{
			maxAttempts: 3,
			conditions:  conditions,
			backoff:     ExponentialBackoff(100 * time.Millisecond), //nolint:mnd // default base backoff duration
		}

		for _, opt := range opts {
			opt(ro)
		}

		o.retry = ro
	}
}

// --- Predefined retry conditions ---

// RetryOnStatusCodes returns a RetryCondition that retries on the given HTTP
// status codes. Useful for 429 (Too Many Requests), 502, 503, 504.
func RetryOnStatusCodes(codes ...int) RetryCondition {
	set := make(map[int]struct{}, len(codes))
	for _, c := range codes {
		set[c] = struct{}{}
	}

	return func(_ *http.Request, resp *http.Response, _ error) bool {
		if resp == nil {
			return false
		}

		_, ok := set[resp.StatusCode]

		return ok
	}
}

// RetryOnNetworkErrors returns a RetryCondition that retries on network-level
// errors (timeout, connection refused, EOF). Does not retry on HTTP errors.
func RetryOnNetworkErrors() RetryCondition {
	return func(_ *http.Request, _ *http.Response, err error) bool {
		return err != nil
	}
}

// RetryOnIdempotentMethods returns a RetryCondition that retries any error
// (network or HTTP) only when the request method is idempotent
// (GET, HEAD, OPTIONS, PUT, DELETE).
//
// Safe to combine with other conditions — it never triggers on POST or PATCH,
// preventing accidental duplication of non-idempotent side effects.
func RetryOnIdempotentMethods() RetryCondition {
	idempotent := map[string]struct{}{
		http.MethodGet:     {},
		http.MethodHead:    {},
		http.MethodOptions: {},
		http.MethodPut:     {},
		http.MethodDelete:  {},
	}

	return func(req *http.Request, _ *http.Response, _ error) bool {
		_, ok := idempotent[req.Method]

		return ok
	}
}

// RetryIf returns a RetryCondition from a custom function.
func RetryIf(fn func(req *http.Request, resp *http.Response, err error) bool) RetryCondition {
	return fn
}

// --- Backoff strategies ---

// BackoffStrategy returns the duration to wait before the nth retry attempt
// (n starts at 1 for the first retry).
type BackoffStrategy func(attempt int) time.Duration

// ConstantBackoff returns a BackoffStrategy that always waits the same duration.
func ConstantBackoff(d time.Duration) BackoffStrategy {
	return func(_ int) time.Duration {
		return d
	}
}

// ExponentialBackoff returns a BackoffStrategy with exponential backoff and
// full jitter: wait = random(0, base * 2^attempt).
// Jitter prevents thundering herds when many clients retry simultaneously.
func ExponentialBackoff(base time.Duration) BackoffStrategy {
	return func(attempt int) time.Duration {
		if base <= 0 {
			return 0
		}

		maxCap := base * (1 << min(attempt, 10))            //nolint:mnd // prevent overflow past 10 doublings
		jitter := time.Duration(rand.Int64N(int64(maxCap))) //nolint:gosec // G404: jitter does not require cryptographic randomness

		return jitter
	}
}

// NoBackoff returns a BackoffStrategy that does not wait between retries.
func NoBackoff() BackoffStrategy {
	return func(_ int) time.Duration {
		return 0
	}
}

// --- retryTransport ---

// retryTransport is an http.RoundTripper that retries requests according to
// the configured conditions and backoff strategy.
type retryTransport struct {
	next http.RoundTripper
	opts *retryOptions
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	bodyBytes, err := bufferBody(req)
	if err != nil {
		return nil, err
	}

	var resp *http.Response

	for attempt := range t.opts.maxAttempts {
		restoreBody(req, bodyBytes)

		resp, err = t.next.RoundTrip(req)

		if attempt == t.opts.maxAttempts-1 || !t.shouldRetry(req, resp, err) {
			break
		}

		drainAndClose(resp)

		if err2 := t.wait(req.Context(), attempt+1); err2 != nil {
			return nil, err2
		}
	}

	return resp, err
}

// bufferBody reads req.Body into memory so it can be replayed on each attempt.
func bufferBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}

	data, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("httpclient: read request body for retry: %w", err)
	}

	req.Body.Close()

	return data, nil
}

// restoreBody resets req.Body from the buffered bytes before each attempt.
func restoreBody(req *http.Request, body []byte) {
	if body != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
}

// drainAndClose drains and closes a response body to allow connection reuse.
func drainAndClose(resp *http.Response) {
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// wait blocks for the backoff duration or until the context is canceled.
func (t *retryTransport) wait(ctx context.Context, attempt int) error {
	d := t.opts.backoff(attempt)
	if d <= 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// shouldRetry returns true if any configured condition triggers a retry.
// Each condition now receives the request so it can make method-aware decisions.
// The built-in safety rule still applies: non-idempotent methods (POST, PATCH)
// are never retried when a response was received, regardless of conditions.
func (t *retryTransport) shouldRetry(req *http.Request, resp *http.Response, err error) bool {
	for _, cond := range t.opts.conditions {
		if !cond(req, resp, err) {
			continue
		}

		// Hard safety gate: if the server responded (resp != nil), never retry
		// POST or PATCH — a response means the server processed the request and
		// retrying could duplicate side effects (payments, emails, etc.).
		if resp != nil {
			switch req.Method {
			case http.MethodPost, http.MethodPatch:
				return false
			}
		}

		return true
	}

	return false
}
