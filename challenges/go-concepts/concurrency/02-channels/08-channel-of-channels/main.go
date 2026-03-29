package main

import (
	"fmt"
	"sync"
	"time"
)

// This program demonstrates passing channels through channels for request-response patterns.
// Run: go run main.go
//
// Expected output:
//   === Example 1: Basic Request-Response ===
//   3 + 4 = 7
//   5 * 6 = 30
//   10 - 3 = 7
//
//   === Example 2: Concurrent Requests ===
//   1 * 1 = 1
//   2 * 2 = 4
//   3 * 3 = 9
//   4 * 4 = 16
//   5 * 5 = 25
//
//   === Example 3: Key-Value Store Service ===
//   GET language: Go (found=true)
//   GET year: 2009 (found=true)
//   GET missing: "" (found=false)
//
//   === Example 4: Bank Account Service ===
//   ... deposits and withdrawals ...
//   Final balance: $100.00

func main() {
	example1BasicRequestResponse()
	example2ConcurrentRequests()
	example3KeyValueStore()
	example4BankAccount()
}

// --- Request and service types ---

// MathRequest embeds a Reply channel: the "return address" for the response.
// Each caller creates its own Reply channel, so responses route to the right place.
type MathRequest struct {
	Op    string
	A, B  float64
	Reply chan float64
}

// mathService runs in a single goroutine, processing requests sequentially.
// Because only this goroutine accesses its internal state, no mutexes are needed.
// This is Go's "share memory by communicating" philosophy in action.
func mathService(requests <-chan MathRequest) {
	for req := range requests {
		var result float64
		switch req.Op {
		case "add":
			result = req.A + req.B
		case "sub":
			result = req.A - req.B
		case "mul":
			result = req.A * req.B
		default:
			result = 0
		}
		// Send the result back on the caller's private reply channel.
		req.Reply <- result
	}
}

// example1BasicRequestResponse shows the simplest request-response flow:
// create a service, send requests with reply channels, receive answers.
func example1BasicRequestResponse() {
	fmt.Println("=== Example 1: Basic Request-Response ===")

	requests := make(chan MathRequest)
	go mathService(requests)

	// Use a buffered reply channel (capacity 1) so the service doesn't block
	// if we're slightly late in receiving the response.
	reply := make(chan float64, 1)

	requests <- MathRequest{Op: "add", A: 3, B: 4, Reply: reply}
	fmt.Printf("3 + 4 = %.0f\n", <-reply)

	requests <- MathRequest{Op: "mul", A: 5, B: 6, Reply: reply}
	fmt.Printf("5 * 6 = %.0f\n", <-reply)

	requests <- MathRequest{Op: "sub", A: 10, B: 3, Reply: reply}
	fmt.Printf("10 - 3 = %.0f\n", <-reply)

	close(requests)
	fmt.Println()
}

// example2ConcurrentRequests demonstrates multiple goroutines sending requests
// to the same service simultaneously. Each goroutine creates its OWN reply channel,
// so responses never get mixed up.
func example2ConcurrentRequests() {
	fmt.Println("=== Example 2: Concurrent Requests ===")

	requests := make(chan MathRequest)
	go mathService(requests)

	var wg sync.WaitGroup
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func(n float64) {
			defer wg.Done()
			// Private reply channel: only this goroutine reads from it.
			reply := make(chan float64, 1)
			requests <- MathRequest{Op: "mul", A: n, B: n, Reply: reply}
			result := <-reply
			fmt.Printf("%.0f * %.0f = %.0f\n", n, n, result)
		}(float64(i))
	}

	wg.Wait()
	close(requests)
	fmt.Println()
}

// --- Key-Value store built entirely on channels ---

type KVResponse struct {
	Value string
	Found bool
}

type KVRequest struct {
	Op    string // "get", "set", "delete"
	Key   string
	Value string // used for "set"
	Reply chan KVResponse
}

// kvService holds a map internally. Only this goroutine reads/writes the map,
// so there are no race conditions -- no sync.Mutex needed.
func kvService(requests <-chan KVRequest) {
	store := make(map[string]string)

	for req := range requests {
		switch req.Op {
		case "set":
			store[req.Key] = req.Value
			req.Reply <- KVResponse{Value: req.Value, Found: true}
		case "get":
			val, ok := store[req.Key]
			req.Reply <- KVResponse{Value: val, Found: ok}
		case "delete":
			delete(store, req.Key)
			req.Reply <- KVResponse{Found: true}
		default:
			req.Reply <- KVResponse{}
		}
	}
}

// Helpers that wrap the channel protocol into a clean function call interface.
func kvSet(requests chan<- KVRequest, key, value string) {
	reply := make(chan KVResponse, 1)
	requests <- KVRequest{Op: "set", Key: key, Value: value, Reply: reply}
	<-reply
}

func kvGet(requests chan<- KVRequest, key string) (string, bool) {
	reply := make(chan KVResponse, 1)
	requests <- KVRequest{Op: "get", Key: key, Reply: reply}
	resp := <-reply
	return resp.Value, resp.Found
}

// example3KeyValueStore shows a realistic concurrent-safe data store built
// entirely with channels. The service goroutine owns the map; clients communicate
// through request/response channels.
func example3KeyValueStore() {
	fmt.Println("=== Example 3: Key-Value Store Service ===")

	requests := make(chan KVRequest)
	go kvService(requests)

	kvSet(requests, "language", "Go")
	kvSet(requests, "year", "2009")

	if val, ok := kvGet(requests, "language"); ok {
		fmt.Printf("GET language: %s (found=%v)\n", val, ok)
	}
	if val, ok := kvGet(requests, "year"); ok {
		fmt.Printf("GET year: %s (found=%v)\n", val, ok)
	}

	val, ok := kvGet(requests, "missing")
	fmt.Printf("GET missing: %q (found=%v)\n", val, ok)

	close(requests)
	fmt.Println()
}

// --- Bank account: a richer request-response example ---

type BankResponse struct {
	Balance float64
	Error   string
}

type BankRequest struct {
	Op     string // "deposit", "withdraw", "balance"
	Amount float64
	Reply  chan BankResponse
}

// bankService holds balance as a local variable. No shared state, no mutex.
// All access is serialized through the request channel.
func bankService(requests <-chan BankRequest) {
	var balance float64

	for req := range requests {
		switch req.Op {
		case "deposit":
			balance += req.Amount
			req.Reply <- BankResponse{Balance: balance}

		case "withdraw":
			if req.Amount > balance {
				req.Reply <- BankResponse{
					Balance: balance,
					Error:   fmt.Sprintf("insufficient funds: have $%.2f, want $%.2f", balance, req.Amount),
				}
			} else {
				balance -= req.Amount
				req.Reply <- BankResponse{Balance: balance}
			}

		case "balance":
			req.Reply <- BankResponse{Balance: balance}

		default:
			req.Reply <- BankResponse{Error: "unknown operation: " + req.Op}
		}
	}
}

// example4BankAccount runs concurrent deposits and withdrawals against a channel-based
// bank account. All operations are race-free because the service goroutine serializes access.
func example4BankAccount() {
	fmt.Println("=== Example 4: Bank Account Service ===")

	requests := make(chan BankRequest)
	go bankService(requests)

	var wg sync.WaitGroup

	// 5 goroutines deposit $100 each.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			reply := make(chan BankResponse, 1)
			requests <- BankRequest{Op: "deposit", Amount: 100, Reply: reply}
			resp := <-reply
			fmt.Printf("Goroutine %d: deposited $100, balance: $%.2f\n", id, resp.Balance)
		}(i)
	}

	// 5 goroutines try to withdraw $80 each.
	for i := 5; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			reply := make(chan BankResponse, 1)
			requests <- BankRequest{Op: "withdraw", Amount: 80, Reply: reply}
			resp := <-reply
			if resp.Error != "" {
				fmt.Printf("Goroutine %d: withdraw failed: %s\n", id, resp.Error)
			} else {
				fmt.Printf("Goroutine %d: withdrew $80, balance: $%.2f\n", id, resp.Balance)
			}
		}(i)
	}

	wg.Wait()

	// Query final balance.
	reply := make(chan BankResponse, 1)
	requests <- BankRequest{Op: "balance", Reply: reply}
	resp := <-reply
	fmt.Printf("Final balance: $%.2f\n", resp.Balance)
	// Expected: 5 * $100 = $500 deposited. Up to 5 * $80 = $400 withdrawn.
	// Actual withdrawals depend on ordering -- some may fail if balance is insufficient.

	close(requests)

	_ = time.Now // keep time import for potential future use
}
