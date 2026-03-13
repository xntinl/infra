# 4. Channel Direction

<!--
difficulty: basic
concepts: [send-only-channel, receive-only-channel, channel-direction, function-signatures]
tools: [go]
estimated_time: 20m
bloom_level: remember
prerequisites: [goroutines, channel-basics, buffered-channels]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [03 - Buffered vs Unbuffered Channels](../03-buffered-vs-unbuffered-channels/03-buffered-vs-unbuffered-channels.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** the syntax for send-only (`chan<- T`) and receive-only (`<-chan T`) channels
- **Use** directional channels in function signatures to express intent
- **Explain** how the compiler enforces channel direction constraints

## Why Channel Direction

When a function only needs to send to a channel, declaring it as `chan<- T` makes that intent explicit. When a function only receives, use `<-chan T`. The compiler enforces these constraints — trying to receive from a send-only channel is a compile error.

Directional channels serve the same purpose as making struct fields unexported: they limit what code can do, preventing accidental misuse. A producer function that accepts `chan<- int` cannot accidentally read from the channel, and a consumer with `<-chan int` cannot accidentally write to it.

Go automatically converts a bidirectional channel to a directional one when passed to a function, so the caller does not need to do anything special.

## Step 1 -- Send-Only Channels

```bash
mkdir -p ~/go-exercises/direction && cd ~/go-exercises/direction
go mod init direction
```

Create `main.go`:

```go
package main

import "fmt"

func produce(ch chan<- int) {
	for i := 1; i <= 5; i++ {
		ch <- i
	}
	close(ch)
}

func main() {
	ch := make(chan int)
	go produce(ch)

	for v := range ch {
		fmt.Println(v)
	}
}
```

The parameter `ch chan<- int` means `produce` can only send to `ch`. The arrow points into `chan`, indicating data flows into the channel. If you tried `<-ch` inside `produce`, the compiler would reject it.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
1
2
3
4
5
```

## Step 2 -- Receive-Only Channels

```go
package main

import "fmt"

func produce(ch chan<- int) {
	for i := 1; i <= 5; i++ {
		ch <- i
	}
	close(ch)
}

func consume(ch <-chan int) {
	for v := range ch {
		fmt.Println("received:", v)
	}
}

func main() {
	ch := make(chan int)
	go produce(ch)
	consume(ch)
}
```

`consume` receives `<-chan int` — it can only read from the channel. Go implicitly converts the bidirectional `chan int` to the directional type when calling each function.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
received: 1
received: 2
received: 3
received: 4
received: 5
```

## Step 3 -- Compiler Enforcement

Try violating the direction constraint:

```go
package main

func wrongReceive(ch chan<- int) {
	_ = <-ch // compile error: cannot receive from send-only channel
}

func wrongSend(ch <-chan int) {
	ch <- 1 // compile error: cannot send to receive-only channel
}

func main() {}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output (two compile errors):

```
./main.go:4:6: invalid operation: cannot receive from send-only channel ch (variable of type chan<- int)
./main.go:8:2: invalid operation: cannot send to receive-only channel ch (variable of type <-chan int)
```

## Step 4 -- Return a Receive-Only Channel

A common pattern is a function that creates a channel internally and returns it as receive-only:

```go
package main

import "fmt"

func generate(nums ...int) <-chan int {
	out := make(chan int)
	go func() {
		for _, n := range nums {
			out <- n
		}
		close(out)
	}()
	return out
}

func main() {
	ch := generate(2, 4, 6, 8)
	for v := range ch {
		fmt.Println(v)
	}
}
```

The caller gets `<-chan int` — it can only receive. The goroutine inside `generate` has the bidirectional channel and can send to it. This is a clean API: the function owns the writing side and the caller owns the reading side.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
2
4
6
8
```

## Step 5 -- Combine Producer and Consumer

```go
package main

import "fmt"

func producer(ch chan<- string) {
	words := []string{"go", "is", "great"}
	for _, w := range words {
		ch <- w
	}
	close(ch)
}

func consumer(ch <-chan string, done chan<- bool) {
	for w := range ch {
		fmt.Println(w)
	}
	done <- true
}

func main() {
	ch := make(chan string)
	done := make(chan bool)

	go producer(ch)
	go consumer(ch, done)

	<-done
}
```

`producer` is send-only on `ch`. `consumer` is receive-only on `ch` and send-only on `done`. Each function has exactly the permissions it needs.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
go
is
great
```

## Common Mistakes

### Closing a Receive-Only Channel

**Wrong:**

```go
func consume(ch <-chan int) {
	close(ch) // compile error
}
```

**Why:** Only the sender should close a channel. A receive-only channel cannot be closed.

### Confusing the Arrow Direction

Remember: the arrow shows data flow direction relative to `chan`.
- `chan<- T` — data flows into the channel (send-only)
- `<-chan T` — data flows out of the channel (receive-only)

## Verify What You Learned

Write a pipeline with three functions:
1. `generate(n int) <-chan int` — produces 1 to n
2. `double(in <-chan int) <-chan int` — doubles each value
3. `main` — prints the doubled values

```bash
go run main.go
```

## What's Next

Continue to [05 - WaitGroup](../05-waitgroup/05-waitgroup.md) to learn a clean way to wait for multiple goroutines without channels.

## Summary

- `chan<- T` is a send-only channel; `<-chan T` is a receive-only channel
- The compiler enforces direction — wrong operations cause compile errors
- Go implicitly converts bidirectional channels to directional when passed to functions
- Returning `<-chan T` from a function exposes only the read side to the caller
- Only the sender should close a channel; closing a receive-only channel is a compile error

## Reference

- [A Tour of Go: Range and Close](https://go.dev/tour/concurrency/4)
- [Go spec: Channel types](https://go.dev/ref/spec#Channel_types)
- [Go Blog: Go Concurrency Patterns: Pipelines](https://go.dev/blog/pipelines)
