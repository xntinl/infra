// Exercise 06: Errgroup Parallel Pipeline
//
// Builds a multi-stage data processing pipeline using errgroup:
//   Producer -> Channel -> Worker Pool (bounded) -> Channel -> Aggregator
//
// A single errgroup.WithContext ties all stages together. If any stage fails,
// the context is cancelled and all stages shut down cooperatively.
//
// Expected output (order of processing lines varies):
//
//   === Order Processing Pipeline ===
//   Processing 8 orders with 3 workers...
//
//     [producer]    sent order 1 (Alice)
//     [producer]    sent order 2 (Bob)
//     ...
//     [worker]      processed order 1: $107.99
//     [worker]      processed order 2: $161.46
//     [aggregator]  collected order 1
//     ...
//
//   Pipeline complete: 8 orders processed
//
//   --- Results ---
//     Order 1 (Alice):   $107.99 [completed]
//     Order 2 (Bob):     $161.46 [completed]
//     ...
//   Total revenue: $XXXX.XX
//
//   === Pipeline with Error ===
//   ...
//   Pipeline error: processing order 4: invalid amount: -50.00
//   Partial results: N orders processed

package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// Order represents an incoming order to process.
type Order struct {
	ID       int
	Customer string
	Amount   float64
}

// ProcessedOrder is the result after validation, tax calculation, etc.
type ProcessedOrder struct {
	ID       int
	Customer string
	Total    float64
	Status   string
}

func main() {
	runSuccessfulPipeline()
	runFailingPipeline()
}

func runSuccessfulPipeline() {
	orders := []Order{
		{ID: 1, Customer: "Alice", Amount: 99.99},
		{ID: 2, Customer: "Bob", Amount: 149.50},
		{ID: 3, Customer: "Charlie", Amount: 29.99},
		{ID: 4, Customer: "Diana", Amount: 250.00},
		{ID: 5, Customer: "Eve", Amount: 75.00},
		{ID: 6, Customer: "Frank", Amount: 199.99},
		{ID: 7, Customer: "Grace", Amount: 50.00},
		{ID: 8, Customer: "Hank", Amount: 320.00},
	}

	fmt.Println("=== Order Processing Pipeline ===")
	fmt.Printf("Processing %d orders with 3 workers...\n\n", len(orders))

	results, err := runPipeline(orders, 3)
	if err != nil {
		fmt.Printf("\nPipeline error: %v\n", err)
		fmt.Printf("Partial results: %d orders processed\n", len(results))
	} else {
		fmt.Printf("\nPipeline complete: %d orders processed\n", len(results))
	}

	printResults(results)
}

func runFailingPipeline() {
	// Order 4 has a negative amount, which processOrder treats as invalid
	orders := []Order{
		{ID: 1, Customer: "Alice", Amount: 99.99},
		{ID: 2, Customer: "Bob", Amount: 149.50},
		{ID: 3, Customer: "Charlie", Amount: 29.99},
		{ID: 4, Customer: "Diana", Amount: -50.00}, // invalid -- triggers error
		{ID: 5, Customer: "Eve", Amount: 75.00},
	}

	fmt.Println("\n=== Pipeline with Error ===")
	fmt.Printf("Processing %d orders (order 4 is invalid)...\n\n", len(orders))

	results, err := runPipeline(orders, 2)
	if err != nil {
		fmt.Printf("\nPipeline error: %v\n", err)
		fmt.Printf("Partial results: %d orders processed\n", len(results))
	} else {
		fmt.Printf("\nPipeline complete: %d orders processed\n", len(results))
	}

	printResults(results)
}

func printResults(results []ProcessedOrder) {
	if len(results) == 0 {
		return
	}
	var totalRevenue float64
	fmt.Println("\n--- Results ---")
	for _, r := range results {
		fmt.Printf("  Order %d (%s): $%.2f [%s]\n", r.ID, r.Customer, r.Total, r.Status)
		totalRevenue += r.Total
	}
	fmt.Printf("Total revenue: $%.2f\n", totalRevenue)
}

// processOrder simulates order processing: validation and tax calculation.
// Negative amounts are treated as invalid.
func processOrder(order Order) (ProcessedOrder, error) {
	delay := time.Duration(50+rand.Intn(100)) * time.Millisecond
	time.Sleep(delay)

	if order.Amount < 0 {
		return ProcessedOrder{}, fmt.Errorf("invalid amount: %.2f", order.Amount)
	}

	taxRate := 0.08
	total := order.Amount * (1 + taxRate)

	return ProcessedOrder{
		ID:       order.ID,
		Customer: order.Customer,
		Total:    total,
		Status:   "completed",
	}, nil
}

// runPipeline orchestrates a three-stage pipeline using a single errgroup.
//
// Architecture:
//   [producer] --ordersCh--> [processorPool] --resultsCh--> [aggregator]
//
// A single errgroup.WithContext ties all stages together:
//   - If the producer fails, context cancels -> workers + aggregator shut down
//   - If a worker fails, context cancels -> producer + aggregator shut down
//   - Channel close propagates "done" signals: producer->workers->aggregator
func runPipeline(orders []Order, numWorkers int) ([]ProcessedOrder, error) {
	g, ctx := errgroup.WithContext(context.Background())

	// Unbuffered channels enforce backpressure between stages.
	// The producer cannot get ahead of the workers.
	ordersCh := make(chan Order)
	resultsCh := make(chan ProcessedOrder)

	var mu sync.Mutex
	var results []ProcessedOrder

	// Stage 1: Producer -- sends orders into the pipeline
	g.Go(func() error {
		return producer(ctx, orders, ordersCh)
	})

	// Stage 2: Worker Pool -- processes orders with bounded parallelism
	g.Go(func() error {
		return processorPool(ctx, numWorkers, ordersCh, resultsCh)
	})

	// Stage 3: Aggregator -- collects processed results
	g.Go(func() error {
		return aggregator(ctx, resultsCh, &mu, &results)
	})

	err := g.Wait()
	return results, err
}

// producer sends each order onto the output channel. It closes the channel when
// done, signaling the worker pool that no more orders are coming.
// The defer close(out) is CRITICAL: without it, the workers' range loop never ends.
func producer(ctx context.Context, orders []Order, out chan<- Order) error {
	defer close(out)

	for _, order := range orders {
		select {
		case <-ctx.Done():
			// A downstream stage failed -- stop producing
			return ctx.Err()
		case out <- order:
			fmt.Printf("  [producer]    sent order %d (%s)\n", order.ID, order.Customer)
		}
	}
	return nil
}

// processorPool reads orders from in, processes each with bounded concurrency,
// and sends results on out. It closes out AFTER all workers finish.
//
// Key design: the range loop runs on the goroutine that called processorPool,
// while g.Go() launches workers. SetLimit ensures at most numWorkers are active.
// close(out) happens AFTER g.Wait() -- closing before Wait would panic if a
// worker tries to send on the closed channel.
func processorPool(ctx context.Context, numWorkers int, in <-chan Order, out chan<- ProcessedOrder) error {
	var g errgroup.Group
	g.SetLimit(numWorkers)

	for order := range in {
		order := order // capture for the closure
		g.Go(func() error {
			// Check for cancellation before doing work
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			result, err := processOrder(order)
			if err != nil {
				return fmt.Errorf("processing order %d: %w", order.ID, err)
			}

			// Send result to aggregator, but respect cancellation
			select {
			case <-ctx.Done():
				return ctx.Err()
			case out <- result:
				fmt.Printf("  [worker]      processed order %d: $%.2f\n", result.ID, result.Total)
			}
			return nil
		})
	}

	// Wait for all workers to finish, THEN close the output channel.
	// This ordering is essential: close before Wait = panic on send.
	err := g.Wait()
	close(out)
	return err
}

// aggregator reads processed orders from the input channel until it is closed.
// It is the only goroutine writing to results, but we use a mutex anyway
// because the caller reads results after Wait() returns, and the mutex
// establishes a clear happens-before relationship.
func aggregator(ctx context.Context, in <-chan ProcessedOrder, mu *sync.Mutex, results *[]ProcessedOrder) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok := <-in:
			if !ok {
				// Channel closed -- all workers are done
				return nil
			}
			mu.Lock()
			*results = append(*results, result)
			mu.Unlock()
			fmt.Printf("  [aggregator]  collected order %d\n", result.ID)
		}
	}
}
