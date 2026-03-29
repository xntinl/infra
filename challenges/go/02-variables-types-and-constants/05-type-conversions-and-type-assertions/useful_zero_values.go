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
	buf.Println(buf.String())

	var mu sync.Mutex
	mu.Lock()
	fmt.Println("Lock acquired")
	mu.UnLock()
	

	var items []String
	items = append(items,"first")
	items = append(items,"second")
	fmt.Println(items)
}




