package main

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

func main() {
	manualWaitGroupErrors()
	errgroupBasic()
	errgroupMultipleErrors()
}

// fetchURL simulates fetching a URL. Returns an error for invalid URLs.
func fetchURL(url string) error {
	time.Sleep(100 * time.Millisecond) // simulate network latency
	if url == "INVALID" {
		return fmt.Errorf("failed to fetch %q: invalid URL", url)
	}
	fmt.Printf("  Fetched: %s\n", url)
	return nil
}

// manualWaitGroupErrors shows the manual pattern: WaitGroup + mutex + error variable.
// This is what errgroup replaces.
func manualWaitGroupErrors() {
	fmt.Println("=== Manual WaitGroup + Error Handling ===")

	urls := []string{
		"https://example.com",
		"https://example.org",
		"INVALID",
		"https://example.net",
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, url := range urls {
		url := url
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fetchURL(url); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	if firstErr != nil {
		fmt.Printf("First error: %v\n", firstErr)
	} else {
		fmt.Println("All tasks succeeded")
	}
}

// errgroupBasic demonstrates the same pattern using errgroup.Group.
// TODO: Create an errgroup.Group, launch tasks with g.Go(), and call g.Wait().
// Compare how much simpler this is than the manual approach above.
func errgroupBasic() {
	fmt.Println("\n=== Errgroup Basic ===")

	urls := []string{
		"https://example.com",
		"https://example.org",
		"INVALID",
		"https://example.net",
	}

	// TODO: Replace this with errgroup.Group
	// Hint: var g errgroup.Group
	var wg sync.WaitGroup

	for _, url := range urls {
		url := url
		// TODO: Use g.Go(func() error { ... }) instead of manual goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = fetchURL(url) // error is silently discarded!
		}()
	}

	// TODO: Replace wg.Wait() with g.Wait() and handle the returned error
	wg.Wait()
	fmt.Println("(error was lost with manual approach)")

	_ = errgroup.Group{} // ensure errgroup import is used
}

// errgroupMultipleErrors shows that Wait() returns only the first error.
// TODO: Launch 5 tasks where tasks 0, 2, 4 fail. Observe that Wait()
// returns only one error.
func errgroupMultipleErrors() {
	fmt.Println("\n=== Errgroup Multiple Errors ===")

	// TODO: Create an errgroup.Group
	// TODO: Launch 5 goroutines with g.Go()
	//   - Each task sleeps for i*50ms to stagger execution
	//   - Tasks where i%2 == 0 return an error
	//   - Tasks where i%2 != 0 print success and return nil
	// TODO: Call g.Wait() and print the returned error

	fmt.Println("TODO: implement this function")
}
