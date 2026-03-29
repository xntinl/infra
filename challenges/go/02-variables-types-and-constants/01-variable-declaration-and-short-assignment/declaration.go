package main

import "fmt"

var maxRetries = 3

func main(){
	var errorCount int
	var ratio float32 = 0.75
	name := "Bob"
	items := []string{"a","b","c"}
	fmt.Println(maxRetries,errorCount,ratio,name,items)
}