package main

import (
	"fmt"
)





func main() {
	services := []ServiceEndpoint{
		{"auth-api",120*time.Millisecond},
		{"payment-gateway",200*time.Millisecond},
		{"notification-svc",80*time.Millisecond},
		{"investiry-api",150*time.Millisecond},
		{"search-engine",90*time.Millisecond},
	}

	fmt.Println("--- sequential health check")
	seqDuration := runSequentialChecks(services)
	fmt.Println("Sequential total: %v\n\n",seqDuration.Round(time.Millisecond))


	fmt.Println("--- concurrent health check")
	concDuration := runConcurrentChecks(services)
	fmt.Println("concurrent total: %v\n\n",concDuration.Round(time.Millisecond))

}
