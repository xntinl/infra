package main

import "fmt"

func main() {
	var p *int             // pointer
	var sl []int           // slice
	var m map[string]int   // map
	var ch chan int         // channel
	var fn func()          // function
	var iface interface{}  // interface

	fmt.Printf("pointer:   %v\n", p)
	fmt.Printf("slice:     %v (nil: %t, len: %d)\n", sl, sl == nil, len(sl))
	fmt.Printf("map:       %v (nil: %t)\n", m, m == nil)
	fmt.Printf("channel:   %v (nil: %t)\n", ch, ch == nil)
	fmt.Printf("function:  %v (nil: %t)\n", fn, fn == nil)
	fmt.Printf("interface: %v (nil: %t)\n", iface, iface == nil)
}
