package main

import (
	"fmt"
	"sync"
	"time"
)

// ============================================================
// Step 1: Request type with response channel
// ============================================================

type MathRequest struct {
	Op    string
	A, B  float64
	Reply chan float64 // the "return address"
}

// ============================================================
// Step 2: Service goroutine
// ============================================================

// mathService processes math requests sequentially.
// It reads requests from the channel and sends results
// back on each request's Reply channel.
func mathService(requests <-chan MathRequest) {
	for req := range requests {
		var result float64
		switch req.Op {
		case "add":
			result = req.A + req.B
		case "mul":
			result = req.A * req.B
		case "sub":
			result = req.A - req.B
		default:
			result = 0
		}
		req.Reply <- result
	}
}

// ============================================================
// Step 3: Send requests and receive responses
// ============================================================

func step3() {
	fmt.Println("--- Step 3: Request-Response ---")

	requests := make(chan MathRequest)
	go mathService(requests)

	// TODO: Create a reply channel
	// reply := make(chan float64)

	// TODO: Send an "add" request for 3 + 4, receive and print result

	// TODO: Send a "mul" request for 5 * 6, receive and print result

	close(requests)
}

// ============================================================
// Step 4: Concurrent requests
// ============================================================

func step4() {
	fmt.Println("--- Step 4: Concurrent Requests ---")

	requests := make(chan MathRequest)
	go mathService(requests)

	var wg sync.WaitGroup

	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func(n float64) {
			defer wg.Done()

			// TODO: Each goroutine creates its OWN reply channel
			// reply := make(chan float64, 1) // buffered to avoid blocking service

			// TODO: Send a "mul" request for n * n

			// TODO: Receive and print: "%.0f * %.0f = %.0f"
			_ = n // remove when used
		}(float64(i))
	}

	wg.Wait()
	close(requests)
}

// ============================================================
// Step 5: Key-Value store service
// ============================================================

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

// kvService holds a map internally. Only this goroutine touches the map.
func kvService(requests <-chan KVRequest) {
	store := make(map[string]string)

	for req := range requests {
		switch req.Op {
		case "set":
			store[req.Key] = req.Value
			req.Reply <- KVResponse{Value: req.Value, Found: true}

		case "get":
			// TODO: Look up key in store, send response with Found=true/false
			val, ok := store[req.Key]
			_ = val // remove when used
			_ = ok  // remove when used

		case "delete":
			// TODO: Delete key, send response
			delete(store, req.Key)

		default:
			req.Reply <- KVResponse{}
		}
	}
}

// kvSet is a helper that sends a "set" request and waits for confirmation.
func kvSet(requests chan<- KVRequest, key, value string) {
	reply := make(chan KVResponse, 1)
	requests <- KVRequest{Op: "set", Key: key, Value: value, Reply: reply}
	<-reply
}

// kvGet is a helper that sends a "get" request and returns the result.
func kvGet(requests chan<- KVRequest, key string) (string, bool) {
	reply := make(chan KVResponse, 1)
	requests <- KVRequest{Op: "get", Key: key, Reply: reply}
	resp := <-reply
	return resp.Value, resp.Found
}

func step5() {
	fmt.Println("--- Step 5: Key-Value Store ---")

	requests := make(chan KVRequest)
	go kvService(requests)

	// TODO: Set a few key-value pairs using kvSet

	// TODO: Get and print values using kvGet

	// TODO: Try getting a key that doesn't exist

	close(requests)
}

// ============================================================
// Final Challenge: Bank Account Service
//
// Request types: "deposit", "withdraw", "balance"
// Response includes new balance and error message
// Service goroutine holds balance as local var
// 10 concurrent goroutines: 5 deposit $100, 5 withdraw $80
// After all ops, query and verify balance
// ============================================================

type BankResponse struct {
	Balance float64
	Error   string // empty if success
}

type BankRequest struct {
	Op     string  // "deposit", "withdraw", "balance"
	Amount float64 // for deposit/withdraw
	Reply  chan BankResponse
}

// bankService holds the balance internally. No shared state.
func bankService(requests <-chan BankRequest) {
	var balance float64

	for req := range requests {
		switch req.Op {
		case "deposit":
			// TODO: Add amount to balance, reply with new balance
			_ = balance // remove when used

		case "withdraw":
			// TODO: If sufficient balance, subtract and reply
			// If insufficient, reply with error

		case "balance":
			// TODO: Reply with current balance

		default:
			req.Reply <- BankResponse{Error: "unknown operation"}
		}
	}
}

func finalChallenge() {
	fmt.Println("--- Final: Bank Account Service ---")

	requests := make(chan BankRequest)
	go bankService(requests)

	var wg sync.WaitGroup

	// 5 goroutines deposit $100 each
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			reply := make(chan BankResponse, 1)

			// TODO: Send deposit request for $100
			// TODO: Receive and print "Goroutine <id>: deposited, balance: <bal>"
			_ = reply // remove when used
			_ = id    // remove when used
		}(i)
	}

	// 5 goroutines withdraw $80 each
	for i := 5; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			reply := make(chan BankResponse, 1)

			// TODO: Send withdraw request for $80
			// TODO: Print result or error
			_ = reply // remove when used
			_ = id    // remove when used
		}(i)
	}

	wg.Wait()

	// Query final balance
	reply := make(chan BankResponse, 1)
	requests <- BankRequest{Op: "balance", Reply: reply}
	resp := <-reply
	fmt.Printf("Final balance: $%.2f\n", resp.Balance)
	// Expected: 5*100 - N*80 where N depends on ordering

	close(requests)
}

func main() {
	step3()
	fmt.Println()

	step4()
	fmt.Println()

	step5()
	fmt.Println()

	finalChallenge()

	_ = time.Now // suppress unused import if needed
}
