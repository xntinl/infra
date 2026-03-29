package main

// Exercise: Race in Closure Loops
// Instructions: see 07-race-in-closure-loops.md

import (
	"fmt"
	"sync"
)

// Step 1a: Implement closureBug.
// Use the range loop correctly with a local capture variable.
// This shows the CORRECT approach -- for comparison with the simulated bug.
func closureBug() {
	fmt.Println("=== Closure -- Correct Capture ===")
	var wg sync.WaitGroup
	values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	for _, v := range values {
		wg.Add(1)
		// TODO: create a local variable val := v
		// TODO: launch a goroutine that prints val (not v)
		_ = v
		go func() {
			defer wg.Done()
			// TODO: fmt.Printf("  goroutine sees: %s\n", val)
		}()
	}

	wg.Wait()
}

// Step 1b: Implement closureBugSimulated.
// Simulate the pre-1.22 bug by using a variable declared OUTSIDE the loop.
// All goroutines will capture the same variable, seeing the last value.
//
// WARNING: This function has an intentional data race.
func closureBugSimulated() {
	fmt.Println("\n=== Simulated Pre-1.22 Bug (intentional race) ===")
	var wg sync.WaitGroup
	values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	// TODO: declare var current string OUTSIDE the loop
	// TODO: in the loop, set current = v, then launch a goroutine
	//       that reads current (all goroutines share the same variable)
	for _, v := range values {
		wg.Add(1)
		_ = v
		go func() {
			defer wg.Done()
			// TODO: fmt.Printf("  goroutine sees: %s\n", current)
		}()
	}

	wg.Wait()
}

// Step 2: Implement closureFixParameter.
// Fix the bug by passing the captured variable as a function parameter.
func closureFixParameter() {
	fmt.Println("\n=== Fix: Pass as Parameter ===")
	var wg sync.WaitGroup
	values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	var current string
	for _, v := range values {
		current = v
		wg.Add(1)
		// TODO: pass current as a parameter to the goroutine function
		// go func(val string) { ... }(current)
		go func() {
			defer wg.Done()
			fmt.Printf("  goroutine sees: %s\n", current) // BUG: still captures current
		}()
	}

	wg.Wait()
}

// Step 3: Implement closureFixLocalVar.
// Fix by declaring a new local variable inside the loop body.
func closureFixLocalVar() {
	fmt.Println("\n=== Fix: Local Variable ===")
	var wg sync.WaitGroup
	values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	var current string
	for _, v := range values {
		current = v
		// TODO: val := current (new variable per iteration)
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Printf("  goroutine sees: %s\n", current) // BUG: still captures current
		}()
	}

	wg.Wait()
}

func main() {
	fmt.Println("=== Race in Closure Loops ===")
	fmt.Println()

	closureBug()
	closureBugSimulated()
	closureFixParameter()
	closureFixLocalVar()

	fmt.Println()
	fmt.Println("Verify: go run -race main.go")
	fmt.Println("Only closureBugSimulated should trigger a DATA RACE warning.")
	fmt.Println()
	fmt.Println("Note: Go 1.22+ creates a new loop variable per iteration,")
	fmt.Println("but the concept still matters for non-loop variables.")
}
