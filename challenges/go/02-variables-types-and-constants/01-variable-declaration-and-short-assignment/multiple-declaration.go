package main

import "fmt"


func main(){
	var (
		firstName string = "Alice"
		lastName string ="Smith"
		age int = 30
	)
	x,y,z := 1,2,3
	a,b:=10,20
	a,b=b,a
	fmt.Printf("Name: %s, %s, Age: %d \n",firstName,lastName, age)
	fmt.Printf("x=%d, y=%d, x=%d\n",x,y,z)
	fmt.Printf("Afeter swap: a=%d b=%d\n",a,b)
}


