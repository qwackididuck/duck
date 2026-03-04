package statemachine_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/qwackididuck/duck/statemachine"
)

// testData is the shared data structure used across test state machines.
type testData struct {
	value   int
	visited []string
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// --- State helpers ---

// stateIncrement increments value and transitions to the next state.
func stateIncrement(next statemachine.StateFunc[*testData]) statemachine.StateFunc[*testData] {
	return func(_ context.Context, d *testData) statemachine.StateFunc[*testData] {
		d.value++
		d.visited = append(d.visited, "increment")

		return next
	}
}

func stateTerminal(_ context.Context, d *testData) statemachine.StateFunc[*testData] {
	d.visited = append(d.visited, "terminal")

	return nil
}

func statePanic(_ context.Context, _ *testData) statemachine.StateFunc[*testData] {
	panic("boom")
}

func stateBlock(ctx context.Context, d *testData) statemachine.StateFunc[*testData] {
	select {
	case <-ctx.Done():
		d.visited = append(d.visited, "blocked-canceled")

		return nil
	case <-time.After(10 * time.Second):
		d.visited = append(d.visited, "blocked-timeout")

		return nil
	}
}

// --- Tests ---

func TestRun_success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		initialState statemachine.StateFunc[*testData]
		initData     *testData
		wantValue    int
		wantVisited  []string
	}{
		{
			name:         "single terminal state",
			initialState: stateTerminal,
			initData:     &testData{},
			wantValue:    0,
			wantVisited:  []string{"terminal"},
		},
		{
			name:         "increment then terminal",
			initialState: stateIncrement(stateTerminal),
			initData:     &testData{},
			wantValue:    1,
			wantVisited:  []string{"increment", "terminal"},
		},
		{
			name:         "two increments then terminal",
			initialState: stateIncrement(stateIncrement(stateTerminal)),
			initData:     &testData{},
			wantValue:    2,
			wantVisited:  []string{"increment", "increment", "terminal"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := statemachine.Run(
				context.Background(),
				tc.initialState,
				tc.initData,
				statemachine.WithLogger(newTestLogger()),
			)
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			if result.value != tc.wantValue {
				t.Errorf("value: expected %d, got %d", tc.wantValue, result.value)
			}

			if len(result.visited) != len(tc.wantVisited) {
				t.Fatalf("visited: expected %v, got %v", tc.wantVisited, result.visited)
			}

			for i, want := range tc.wantVisited {
				if result.visited[i] != want {
					t.Errorf("visited[%d]: expected %q, got %q", i, want, result.visited[i])
				}
			}
		})
	}
}

func TestRun_panicRecovery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		initialState statemachine.StateFunc[*testData]
		wantErr      error
	}{
		{
			name:         "panic in first state returns ErrPanic",
			initialState: statePanic,
			wantErr:      statemachine.ErrPanic,
		},
		{
			name:         "panic after successful state returns ErrPanic",
			initialState: stateIncrement(statePanic),
			wantErr:      statemachine.ErrPanic,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := statemachine.Run(
				context.Background(),
				tc.initialState,
				&testData{},
				statemachine.WithLogger(newTestLogger()),
			)

			if !errors.Is(err, tc.wantErr) {
				t.Errorf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestRun_contextCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setupCtx func() (context.Context, context.CancelFunc)
		wantErr  error
	}{
		{
			name: "already canceled context returns ErrContextDone",
			setupCtx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				return ctx, cancel
			},
			wantErr: statemachine.ErrContextDone,
		},
		{
			name: "context canceled during blocking state returns ErrContextDone",
			setupCtx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 50*time.Millisecond)
			},
			wantErr: statemachine.ErrContextDone,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := tc.setupCtx()
			defer cancel()

			_, err := statemachine.Run(
				ctx,
				stateBlock,
				&testData{},
				statemachine.WithLogger(newTestLogger()),
			)

			if !errors.Is(err, tc.wantErr) {
				t.Errorf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestRun_withTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		timeout      time.Duration
		initialState statemachine.StateFunc[*testData]
		wantErr      error
		wantNoErr    bool
	}{
		{
			name:         "timeout exceeded on blocking state",
			timeout:      50 * time.Millisecond,
			initialState: stateBlock,
			wantErr:      statemachine.ErrContextDone,
		},
		{
			name:         "generous timeout completes successfully",
			timeout:      5 * time.Second,
			initialState: stateTerminal,
			wantNoErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := statemachine.Run(
				context.Background(),
				tc.initialState,
				&testData{},
				statemachine.WithTimeout(tc.timeout),
				statemachine.WithLogger(newTestLogger()),
			)

			if tc.wantNoErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}

			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}
