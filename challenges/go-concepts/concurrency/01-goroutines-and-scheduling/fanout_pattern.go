package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)



func main() {
	n := 10
	fmt.Printf("=== Fan-out: %d goroutines === \n",n)
	var wg sync.WaitGroup


	for i := 0; i<n; i++ {
		wg.Add(1)
		go func(index int){
			defer wg.Done()
			fmt.Printf("goroutine %d/%d starting\n",index,n)
			sleepMs := rand.Intn(100)
			time.Sleep(time.Duration(sleepMs)*time.Millisecond)
			fmt.Printf("goroutine %d/%d done (took %dms) \n",index,n,sleepMs)
		}(i)
	}

	wg.Wait()
	fmt.Printf("All %d goroutines completed.\n",n)

}



