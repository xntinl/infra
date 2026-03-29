package main

import (
	"fmt"
	"sync"
	"time"
)

func printNumbers(label string){

		for i:=0; i<5; i++{
			fmt.Print("%s-%d",label,i)
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
	start:= time.Now()

	var wg sync.WaitGroup


}

