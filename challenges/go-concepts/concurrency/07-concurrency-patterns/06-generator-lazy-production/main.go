package main

// Generator: Lazy Production -- Complete Working Example
//
// A generator is a function that returns <-chan T and produces values in
// a background goroutine. The consumer drives the pace: if the consumer
// stops reading, the generator blocks on send. This is lazy evaluation
// through backpressure.
//
// Expected output:
//   Range [1,5]: 1 2 3 4 5
//
//   First 10 Fibonacci: [0 1 1 2 3 5 8 13 21 34]
//
//   First 10 Fibonacci (cancelable): [0 1 1 2 3 5 8 13 21 34]
//
//   Squares: [0 1 4 9 16 25 36 49]
//   Powers of 2: [1 2 4 8 16 32 64 128]
//
//   First 15 primes: [2 3 5 7 11 13 17 19 23 29 31 37 41 43 47]

import "fmt"

// ---------------------------------------------------------------------------
// rangeGen: finite generator that produces integers from start to end.
// The unbuffered channel means values are produced lazily -- the goroutine
// blocks on each send until the consumer reads.
// ---------------------------------------------------------------------------

func rangeGen(start, end int) <-chan int {
	out := make(chan int)
	go func() {
		for i := start; i <= end; i++ {
			out <- i
		}
		close(out) // Signal: no more values. Lets `range` loop exit.
	}()
	return out
}

// ---------------------------------------------------------------------------
// fibonacci: infinite generator for the Fibonacci sequence.
// The goroutine runs forever, but it only produces a value when the
// consumer is ready to receive. No CPU or memory is wasted on values
// that will never be consumed.
//
// WARNING: This version leaks the goroutine after the consumer stops
// reading. The goroutine blocks on `out <- a` with no way to unblock.
// See fibonacciWithDone below for the fix.
// ---------------------------------------------------------------------------

func fibonacci() <-chan int {
	out := make(chan int)
	go func() {
		a, b := 0, 1
		for {
			out <- a
			a, b = b, a+b
		}
		// Never closes -- this goroutine runs until the process exits
		// or (in the leak case) it blocks forever on send.
	}()
	return out
}

// ---------------------------------------------------------------------------
// take: consumes exactly n values from a channel.
// This is the standard way to limit consumption from an infinite generator.
// ---------------------------------------------------------------------------

func take(n int, in <-chan int) []int {
	result := make([]int, 0, n)
	for i := 0; i < n; i++ {
		v, ok := <-in
		if !ok {
			break // channel was closed before we got n values
		}
		result = append(result, v)
	}
	return result
}

// ---------------------------------------------------------------------------
// fibonacciWithDone: production-quality infinite generator.
// Accepts a done channel for cancellation. The select statement lets the
// goroutine exit cleanly when the consumer closes done.
//
// Rule: EVERY infinite generator must accept a cancellation signal.
// Without it, you leak goroutines in long-running programs.
// ---------------------------------------------------------------------------

func fibonacciWithDone(done <-chan struct{}) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out) // Single exit path with defer.
		a, b := 0, 1
		for {
			select {
			case out <- a:
				// Consumer received the value. Advance the sequence.
				a, b = b, a+b
			case <-done:
				// Consumer is done. Exit the goroutine cleanly.
				return
			}
		}
	}()
	return out
}

// ---------------------------------------------------------------------------
// generateFrom: higher-order generator.
// Accepts a function fn(index)->value and produces fn(0), fn(1), fn(2)...
// until done is closed. This is the general-purpose generator factory.
// ---------------------------------------------------------------------------

func generateFrom(done <-chan struct{}, fn func(int) int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		i := 0
		for {
			select {
			case out <- fn(i):
				i++
			case <-done:
				return
			}
		}
	}()
	return out
}

// ---------------------------------------------------------------------------
// Prime sieve: a chain of goroutines, each filtering out multiples of a
// prime. This is the classic concurrent prime sieve from Hoare's CSP.
//
//   numbers(2,3,4,...) -> filter(2) -> filter(3) -> filter(5) -> ...
//                          |            |            |
//                          2            3            5     (primes)
//
// Each time we read a value from the chain, it is prime (it survived all
// previous filters). We then attach a new filter for that prime.
// ---------------------------------------------------------------------------

// naturalsFrom produces an infinite stream of integers starting at start.
func naturalsFrom(done <-chan struct{}, start int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for i := start; ; i++ {
			select {
			case out <- i:
			case <-done:
				return
			}
		}
	}()
	return out
}

// filterMultiples forwards only values not divisible by prime.
func filterMultiples(done <-chan struct{}, in <-chan int, prime int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for {
			select {
			case n, ok := <-in:
				if !ok {
					return
				}
				if n%prime != 0 {
					select {
					case out <- n:
					case <-done:
						return
					}
				}
			case <-done:
				return
			}
		}
	}()
	return out
}

// primes returns the first n primes using a sieve of goroutines.
func primes(n int) []int {
	result := make([]int, 0, n)
	done := make(chan struct{})

	// Start with the stream 2, 3, 4, 5, ...
	ch := naturalsFrom(done, 2)

	for i := 0; i < n; i++ {
		// The first value to survive all filters is prime.
		prime := <-ch
		result = append(result, prime)
		// Attach a new filter that removes multiples of this prime.
		ch = filterMultiples(done, ch, prime)
	}

	// Clean up all goroutines in the sieve chain.
	close(done)
	return result
}

func main() {
	fmt.Println("Exercise: Generator -- Lazy Production")
	fmt.Println()

	// Finite generator
	fmt.Print("Range [1,5]: ")
	for v := range rangeGen(1, 5) {
		fmt.Printf("%d ", v)
	}
	fmt.Println()
	fmt.Println()

	// Infinite generator (leaks -- educational, see Step 3 for fix)
	fmt.Printf("First 10 Fibonacci: %v\n\n", take(10, fibonacci()))

	// Cancelable infinite generator (no leak)
	done := make(chan struct{})
	fib := fibonacciWithDone(done)
	result := take(10, fib)
	close(done) // Signal generator to stop -- goroutine exits cleanly.
	fmt.Printf("First 10 Fibonacci (cancelable): %v\n\n", result)

	// Higher-order generators
	done2 := make(chan struct{})
	squares := generateFrom(done2, func(i int) int { return i * i })
	fmt.Printf("Squares: %v\n", take(8, squares))
	close(done2)

	done3 := make(chan struct{})
	powersOf2 := generateFrom(done3, func(i int) int {
		r := 1
		for j := 0; j < i; j++ {
			r *= 2
		}
		return r
	})
	fmt.Printf("Powers of 2: %v\n\n", take(8, powersOf2))
	close(done3)

	// Prime sieve
	fmt.Printf("First 15 primes: %v\n", primes(15))
}
