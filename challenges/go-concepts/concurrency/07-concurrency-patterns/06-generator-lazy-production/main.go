package main

// Exercise: Generator -- Lazy Production
// Instructions: see 06-generator-lazy-production.md

import "fmt"

// Step 1: Implement rangeGen.
// Returns a channel that lazily produces integers from start to end (inclusive).
// Close the channel after the last value.
func rangeGen(start, end int) <-chan int {
	out := make(chan int)
	// TODO: launch goroutine that sends start..end, then closes out
	go func() {
		close(out)
	}()
	return out
}

// Step 2: Implement fibonacci (infinite generator).
// Returns a channel that produces Fibonacci numbers forever.
// The goroutine blocks on send when the consumer is not reading.
func fibonacci() <-chan int {
	out := make(chan int)
	// TODO: launch goroutine: a, b := 0, 1; loop forever sending a, then a, b = b, a+b
	go func() {
		close(out)
	}()
	return out
}

// take consumes exactly n values from a channel.
func take(n int, in <-chan int) []int {
	result := make([]int, 0, n)
	// TODO: read n values from in (break if channel closes early)
	_ = in
	return result
}

// Step 3: Implement fibonacciWithDone.
// Same as fibonacci but accepts a done channel for cancellation.
// Use select to listen for both send and done.
func fibonacciWithDone(done <-chan struct{}) <-chan int {
	out := make(chan int)
	// TODO: launch goroutine with select { case out <- a: ... case <-done: return }
	// TODO: defer close(out) inside the goroutine
	go func() {
		defer close(out)
		_ = done
	}()
	return out
}

// Step 4: Implement generateFrom (higher-order generator).
// Accepts a done channel and a function fn(index) -> value.
// Produces fn(0), fn(1), fn(2), ... until done is closed.
func generateFrom(done <-chan struct{}, fn func(int) int) <-chan int {
	out := make(chan int)
	// TODO: launch goroutine with index counter, select on out <- fn(i) and <-done
	go func() {
		defer close(out)
		_ = done
		_ = fn
	}()
	return out
}

// Verify: Implement a prime sieve generator.
// filterMultiples reads from in and forwards only values not divisible by prime.
func filterMultiples(done <-chan struct{}, in <-chan int, prime int) <-chan int {
	out := make(chan int)
	// TODO: launch goroutine that ranges over in, forwards n if n%prime != 0
	// TODO: use select with done for cancellation
	go func() {
		defer close(out)
		_ = done
		_ = prime
	}()
	return out
}

// primes returns the first n primes using a sieve of goroutines.
func primes(n int) []int {
	result := make([]int, 0, n)
	// TODO: create done channel
	// TODO: create a generator for 2, 3, 4, 5, ...
	// TODO: loop n times: read a prime, add filterMultiples stage to the chain
	// TODO: close done to clean up all goroutines
	_ = n
	return result
}

func main() {
	fmt.Println("Exercise: Generator -- Lazy Production\n")

	// Step 1: finite generator
	fmt.Print("Range [1,5]: ")
	for v := range rangeGen(1, 5) {
		fmt.Printf("%d ", v)
	}
	fmt.Println("\n")

	// Step 2: infinite generator (leaks goroutine -- fixed in Step 3)
	fmt.Printf("First 10 Fibonacci: %v\n\n", take(10, fibonacci()))

	// Step 3: cancelable generator
	done := make(chan struct{})
	fib := fibonacciWithDone(done)
	result := take(10, fib)
	close(done) // signal generator to stop
	fmt.Printf("First 10 Fibonacci (cancelable): %v\n\n", result)

	// Step 4: higher-order generators
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

	// Verify: prime sieve
	fmt.Printf("First 15 primes: %v\n", primes(15))
}
