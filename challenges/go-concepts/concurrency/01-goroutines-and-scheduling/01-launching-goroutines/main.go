package main

import (
	"fmt"
	"sync"
	"time"
)

func printNumbers(label string){

		for i:=0; i<5; i++{
			fmt.Printf("%s-%d",label,i)
			time.Sleep(20*time.Millisecond)
		}
		fmt.Println()
}


func main() {
	fmt.Println("--- Sequential ---")
	start := time.Now()

	printNumbers("A")
	printNumbers("B")
	printNumbers("C")

	fmt.Printf("Sequential took: %v\n\n",time.Since(start).Round(time.Millisecond))


	fmt.Println("--- Concurrent ---")
	start = time.Now()

	var wg sync.WaitGroup

	for _, label := range []string{"A","B","C"} {
		wg.Add(1)
		go func(l string){
			defer wg.Done()
			printNumbers(l)
		}(label)
	}
	wg.Wait()

	fmt.Printf("Concurrent took: %v\n",time.Since(start).Round(time.Millisecond))

}

