package main


import (
	"bytes"
	"fmt"
	"sync"
)

func main() {
	var buf bytes.Buffer
	buf.WriteString("Hello, ")
	buf.WriteString("world!")
	fmt.Println(buf.String())

	var mu sync.Mutex
	mu.Lock()
	fmt.Println("Lock acquired")
	mu.Unlock()
	

	var items []string
	items = append(items,"first")
	items = append(items,"second")
	fmt.Println(items)
}




