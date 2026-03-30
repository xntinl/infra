package main


import (
	"fmt"
	"sync"
)

func main(){
	var wg sync.WaitGroup
	for i:=0; i<5; i++{
		wg.Add(1)
		go func(n int){
			defer wg.Done()
			fmt.Printf("goroutine received: %d\n",n)
		}(i)
	}

	wg.Wait()
	fmt.Println("All values 0-4 appear exactly once (in any order).")
}
