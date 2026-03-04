// Package statemachine provides a generic state machine engine.
//
// The engine runs a chain of state functions, each returning the next state
// to execute, until a state returns nil (terminal state). It handles context
// cancellation, optional timeout, and panic recovery.
//
// Usage:
//
//	type exchangeData struct {
//	    req  *Request
//	    resp Response
//	}
//
//	func (s *MyService) validate(ctx context.Context, d *exchangeData) statemachine.StateFunc[*exchangeData] {
//	    if d.req.ID == "" {
//	        d.resp = errorResponse(ErrInvalidID)
//	        return nil
//	    }
//	    return s.process
//	}
//
//	data, err := statemachine.Run(ctx, s.validate, &exchangeData{req: req},
//	    statemachine.WithTimeout(30 * time.Second),
//	)
package statemachine

import (
	"log/slog"
	"time"
)

// options holds the engine configuration.
type options struct {
	timeout time.Duration
	logger  *slog.Logger
}

// defaultOptions returns the default engine options.
func defaultOptions() options {
	return options{
		logger: slog.Default(),
	}
}

// Option is a functional option for configuring the state machine engine.
type Option func(*options)

// WithTimeout sets a deadline for the entire state machine execution.
// If the timeout is reached before the machine terminates, Run returns
// a wrapped [context.DeadlineExceeded] error.
//
// This is additive with any deadline already set on the context passed to Run.
// The stricter of the two deadlines will apply.
func WithTimeout(d time.Duration) Option {
	return func(o *options) {
		o.timeout = d
	}
}

// WithLogger sets the logger used by the engine for internal events
// (panic recovery, timeout). It does not affect state-level logging,
// which is the responsibility of the state functions themselves.
// Defaults to [slog.Default].
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		l := logger
		if l == nil {
			l = slog.Default()
		}

		o.logger = l
	}
}
