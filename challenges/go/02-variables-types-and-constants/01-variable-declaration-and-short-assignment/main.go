package main

import "fmt"


func main(){
	name := "Alice"
	age := 30
	height := 5.9
	active := true


	fmt.Printf("Name: %s (type: %T)\n",name,name)
	fmt.Printf("Age: %d (type: %T)\n",age,age)
	fmt.Printf("Height: %.1f (type:  %T)\n",height,height)
	fmt.Printf("Active: %t (type: %T)\n",active,active)
}