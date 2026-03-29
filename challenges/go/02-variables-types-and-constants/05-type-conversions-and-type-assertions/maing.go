package main

import "fmt"

func main(){
	var i int
	var f float64
	var b bool
	var s string
	var by byte
	var r rune
	fmt.Printf("int: %d (repr: %v)\n",i,i)
	fmt.Printf("float64: %f (repr: %v)\n",f,f)
	fmt.Printf("bool: %t (repr: %v)\n",b,b)
	fmt.Printf("string: %q (repr: %v)\n",s,s)
	fmt.Printf("byte: %d (repr: %v)\n",by,by)
	fmt.Printf("rune: %d (repr: %v)\n",r,r)
}