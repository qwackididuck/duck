package statemachine

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
)

// StateFunc is a function that represents a single state in the machine.
// It receives the current context and the shared data, performs its work,
// and returns the next state to execute, or nil to terminate the machine.
//
// States should respect context cancellation by checking ctx.Done() before
// performing long-running or blocking operations.
type StateFunc[D any] func(ctx context.Context, data D) StateFunc[D]

// ErrPanic is returned by Run when a state function panics.
// The original panic value and stack trace are included in the error message.
var ErrPanic = errors.New("state machine panic")

// ErrContextDone is returned by Run when the context is canceled or its
// deadline is exceeded before the machine reaches a terminal state.
var ErrContextDone = errors.New("state machine context done")

// Run executes the state machine starting from initialState with the provided
// data. It blocks until the machine terminates, the context is done, or a
// panic is recovered.
//
// Returns the final data and nil on success.
// Returns the zero value of D and a non-nil error on failure.
func Run[D any](ctx context.Context, initialState StateFunc[D], data D, opts ...Option) (D, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}

	runCtx := ctx

	if o.timeout > 0 {
		var cancel context.CancelFunc

		runCtx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}

	resultCh := make(chan execResult[D], 1)

	go func() {
		resultCh <- execute(runCtx, initialState, data, o)
	}()

	select {
	case res := <-resultCh:
		return res.data, res.err

	case <-runCtx.Done():
		var zero D

		o.logger.WarnContext(runCtx, "state machine interrupted",
			"reason", runCtx.Err().Error(),
		)

		return zero, fmt.Errorf("%w: %w", ErrContextDone, runCtx.Err())
	}
}

// execResult holds the outcome of a state machine execution.
type execResult[D any] struct {
	data D
	err  error
}

// execute runs the state loop and recovers from panics.
// It is always called in a separate goroutine by Run.
//
//nolint:nonamedreturns
func execute[D any](ctx context.Context, initialState StateFunc[D], data D, o options) (res execResult[D]) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			o.logger.ErrorContext(ctx, "state machine panic recovered",
				"panic", fmt.Sprintf("%v", r),
				"stack", string(stack),
			)

			var zero D

			res = execResult[D]{
				data: zero,
				err:  fmt.Errorf("%w: %v", ErrPanic, r),
			}
		}
	}()

	for state := initialState; state != nil; {
		select {
		case <-ctx.Done():
			var zero D

			return execResult[D]{
				data: zero,
				err:  fmt.Errorf("%w: %w", ErrContextDone, ctx.Err()),
			}
		default:
		}

		state = state(ctx, data)
	}

	return execResult[D]{data: data, err: nil}
}
