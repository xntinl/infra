package main


import "fmt"


func compute() int { return 42}



func main(){
	ch := make(chan int)
	go func(){
		ch <- compute()
	}()
	result := <-ch
	fmt.Println(result)
}


