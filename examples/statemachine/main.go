// Example: order processing pipeline using a state machine.
// Each state is a function that returns the next state, or nil to terminate.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/qwackididuck/duck/statemachine"
)

// OrderData is the shared data that flows through the state machine.
type OrderData struct {
	OrderID  string
	Total    float64
	Email    string
	Paid     bool
	Notified bool
	Error    string
}

// States are plain functions — no interface to implement.

func validateOrder(_ context.Context, data OrderData) statemachine.StateFunc[OrderData] {
	fmt.Printf("[validate] order=%s total=%.2f\n", data.OrderID, data.Total)

	if data.Total <= 0 {
		data.Error = "invalid total"

		return nil // terminal — validation failed
	}

	if data.Email == "" {
		data.Error = "missing email"

		return nil
	}

	return processPayment
}

func processPayment(ctx context.Context, data OrderData) statemachine.StateFunc[OrderData] {
	fmt.Printf("[payment] charging %.2f for order=%s\n", data.Total, data.OrderID)

	// Simulate payment processing
	select {
	case <-ctx.Done():
		return nil // canceled — state machine will return ErrContextDone
	case <-time.After(50 * time.Millisecond):
	}

	data.Paid = true

	return sendConfirmation
}

func sendConfirmation(ctx context.Context, data OrderData) statemachine.StateFunc[OrderData] {
	fmt.Printf("[notify] sending confirmation to %s\n", data.Email)

	data.Notified = true

	return nil // terminal — all done
}

func main() {
	// --- Happy path ---
	ctx := context.Background()

	result, err := statemachine.Run(ctx, validateOrder, OrderData{
		OrderID: "ord_001",
		Total:   99.99,
		Email:   "alice@example.com",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "state machine error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("done: paid=%v notified=%v\n\n", result.Paid, result.Notified)

	// --- Validation failure ---
	result, err = statemachine.Run(ctx, validateOrder, OrderData{
		OrderID: "ord_002",
		Total:   -5,
	})

	fmt.Printf("invalid order: err=%v data.Error=%q\n\n", err, result.Error)

	// --- Context cancellation ---
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = statemachine.Run(ctx, validateOrder, OrderData{
		OrderID: "ord_003",
		Total:   49.99,
		Email:   "bob@example.com",
	})

	if errors.Is(err, statemachine.ErrContextDone) {
		fmt.Println("context canceled as expected")
	}

	// --- With options ---
	result, err = statemachine.Run(
		context.Background(),
		validateOrder,
		OrderData{OrderID: "ord_004", Total: 9.99, Email: "carol@example.com"},
		statemachine.WithTimeout(5*time.Second),
	)

	fmt.Printf("with timeout: paid=%v err=%v\n", result.Paid, err)
}
