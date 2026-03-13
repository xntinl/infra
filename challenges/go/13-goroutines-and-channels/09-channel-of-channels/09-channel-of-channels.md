# 9. Channel of Channels

<!--
difficulty: intermediate
concepts: [chan-chan, request-response-pattern, reply-channel, service-goroutine]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, channel-basics, channel-direction]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [04 - Channel Direction](../04-channel-direction/04-channel-direction.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** the `chan chan T` pattern for request-response communication
- **Implement** a service goroutine that processes requests with reply channels
- **Design** goroutine interactions using reply channels

## Why Channel of Channels

Sometimes a goroutine needs to send a request and receive a response. A plain channel goes one way. The channel-of-channels pattern solves this: the requester creates a reply channel, sends it to the service goroutine along with the request, and then waits on the reply channel for the answer.

This pattern appears in real systems where a single goroutine owns a resource (a cache, a connection, a state machine) and other goroutines interact with it through requests.

## Step 1 -- Basic Request-Response

```bash
mkdir -p ~/go-exercises/chan-chan && cd ~/go-exercises/chan-chan
go mod init chan-chan
```

Create `main.go`:

```go
package main

import "fmt"

type Request struct {
	Value    int
	Response chan int
}

func doubler(requests <-chan Request) {
	for req := range requests {
		req.Response <- req.Value * 2
	}
}

func main() {
	requests := make(chan Request)
	go doubler(requests)

	response := make(chan int)
	requests <- Request{Value: 21, Response: response}
	fmt.Println("21 * 2 =", <-response)

	requests <- Request{Value: 7, Response: response}
	fmt.Println("7 * 2 =", <-response)

	close(requests)
}
```

Each request carries its own reply channel. The `doubler` service reads requests, computes the result, and sends it back on the reply channel.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
21 * 2 = 42
7 * 2 = 14
```

## Step 2 -- Concurrent Requests

Multiple goroutines can send requests concurrently, each with its own reply channel:

```go
package main

import (
	"fmt"
	"sync"
)

type Request struct {
	A, B     int
	Response chan int
}

func adder(requests <-chan Request) {
	for req := range requests {
		req.Response <- req.A + req.B
	}
}

func main() {
	requests := make(chan Request)
	go adder(requests)

	var wg sync.WaitGroup
	pairs := [][2]int{{1, 2}, {10, 20}, {100, 200}}

	for _, pair := range pairs {
		wg.Add(1)
		go func(a, b int) {
			defer wg.Done()
			reply := make(chan int)
			requests <- Request{A: a, B: b, Response: reply}
			fmt.Printf("%d + %d = %d\n", a, b, <-reply)
		}(pair[0], pair[1])
	}

	wg.Wait()
	close(requests)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (order may vary):

```
1 + 2 = 3
10 + 20 = 30
100 + 200 = 300
```

## Step 3 -- Channel of Channels (Literal)

A `chan chan int` is a channel that carries channels:

```go
package main

import "fmt"

func server(cc chan chan int) {
	counter := 0
	for reply := range cc {
		counter++
		reply <- counter
	}
}

func main() {
	cc := make(chan chan int)
	go server(cc)

	for i := 0; i < 5; i++ {
		reply := make(chan int)
		cc <- reply
		fmt.Println("counter:", <-reply)
	}

	close(cc)
}
```

The server goroutine owns the counter state. Each client creates a reply channel, sends it through `cc`, and reads the response.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
counter: 1
counter: 2
counter: 3
counter: 4
counter: 5
```

## Step 4 -- Request-Response with Errors

```go
package main

import (
	"errors"
	"fmt"
)

type CalcRequest struct {
	A, B     int
	Op       string
	Response chan CalcResult
}

type CalcResult struct {
	Value int
	Err   error
}

func calculator(requests <-chan CalcRequest) {
	for req := range requests {
		var result CalcResult
		switch req.Op {
		case "+":
			result.Value = req.A + req.B
		case "-":
			result.Value = req.A - req.B
		case "*":
			result.Value = req.A * req.B
		case "/":
			if req.B == 0 {
				result.Err = errors.New("division by zero")
			} else {
				result.Value = req.A / req.B
			}
		default:
			result.Err = fmt.Errorf("unknown operation: %s", req.Op)
		}
		req.Response <- result
	}
}

func calc(requests chan<- CalcRequest, a, b int, op string) {
	reply := make(chan CalcResult)
	requests <- CalcRequest{A: a, B: b, Op: op, Response: reply}
	result := <-reply
	if result.Err != nil {
		fmt.Printf("%d %s %d = error: %v\n", a, op, b, result.Err)
	} else {
		fmt.Printf("%d %s %d = %d\n", a, op, b, result.Value)
	}
}

func main() {
	requests := make(chan CalcRequest)
	go calculator(requests)

	calc(requests, 10, 3, "+")
	calc(requests, 10, 3, "-")
	calc(requests, 10, 3, "*")
	calc(requests, 10, 3, "/")
	calc(requests, 10, 0, "/")
	calc(requests, 10, 3, "%")

	close(requests)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
10 + 3 = 13
10 - 3 = 7
10 * 3 = 30
10 / 3 = 3
10 / 0 = error: division by zero
10 % 3 = error: unknown operation: %
```

## Step 5 -- Key-Value Store with Channel Requests

```go
package main

import "fmt"

type GetRequest struct {
	Key   string
	Reply chan string
}

type SetRequest struct {
	Key   string
	Value string
	Reply chan bool
}

func kvStore(gets <-chan GetRequest, sets <-chan SetRequest) {
	store := make(map[string]string)
	for {
		select {
		case req, ok := <-gets:
			if !ok {
				return
			}
			req.Reply <- store[req.Key]
		case req, ok := <-sets:
			if !ok {
				return
			}
			store[req.Key] = req.Value
			req.Reply <- true
		}
	}
}

func main() {
	gets := make(chan GetRequest)
	sets := make(chan SetRequest)
	go kvStore(gets, sets)

	// Set values
	for _, kv := range [][2]string{{"name", "Go"}, {"version", "1.22"}} {
		reply := make(chan bool)
		sets <- SetRequest{Key: kv[0], Value: kv[1], Reply: reply}
		<-reply
	}

	// Get values
	for _, key := range []string{"name", "version", "missing"} {
		reply := make(chan string)
		gets <- GetRequest{Key: key, Reply: reply}
		val := <-reply
		if val == "" {
			fmt.Printf("%s: (not found)\n", key)
		} else {
			fmt.Printf("%s: %s\n", key, val)
		}
	}

	close(gets)
	close(sets)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
name: Go
version: 1.22
missing: (not found)
```

## Common Mistakes

### Forgetting to Create a Reply Channel per Request

Reusing a single reply channel across concurrent requests causes data races. Each request must have its own reply channel.

### Not Closing the Reply Channel

For single-value responses, the reply channel is garbage collected after both sides are done. You do not need to close it explicitly — closing is for signaling "no more values."

## Verify What You Learned

Build a "unique ID generator" service goroutine:
1. It accepts `chan chan int`
2. Each time it receives a reply channel, it sends the next unique integer
3. Five concurrent goroutines each request an ID and print it

## What's Next

Continue to [10 - Signaling with Closed Channels](../10-signaling-with-closed-channels/10-signaling-with-closed-channels.md) to learn how closing a channel acts as a broadcast signal.

## Summary

- `chan chan T` lets goroutines send request-response pairs through channels
- Each request carries its own reply channel for the response
- A service goroutine can own state safely without mutexes by processing requests serially
- Always create a new reply channel per request in concurrent scenarios
- This pattern is the basis for actor-like concurrency in Go

## Reference

- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Rob Pike: Concurrency Is Not Parallelism](https://go.dev/talks/2012/waza.slide)
