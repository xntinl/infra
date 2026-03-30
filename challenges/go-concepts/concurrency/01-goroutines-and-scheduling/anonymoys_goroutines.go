package main

import (
	"fmt"
	"sync"
)

func main()  {
	var wg sync.WaitGroup
	wg.Add(1)

	go func(){
		defer wg.Done()
		fmt.Println("anonymous goroutine (no params)")

	}()

	wg.Add(1)
	go func(msg string, n int){
		defer wg.Done()
		fmt.Printf("anonymous goroutine: msg=%q, n=%d\n",msg,n)

	}("hello",42)

	wg.Wait()

}
