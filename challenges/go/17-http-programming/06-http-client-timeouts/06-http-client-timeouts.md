# 6. HTTP Client Timeouts

<!--
difficulty: intermediate
concepts: [http-client-timeout, transport-settings, context-timeout, dial-timeout, tls-timeout]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [http-client, context, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [02 - HTTP Client](../02-http-client/02-http-client.md)
- Familiarity with `context.Context`

## Learning Objectives

After completing this exercise, you will be able to:

- **Configure** `http.Client` with overall and per-phase timeouts
- **Use** `context.WithTimeout` for per-request deadlines
- **Analyze** which timeout layer triggered a failure

## Why HTTP Client Timeouts

The default `http.Client` has no timeout. A request to a slow or unresponsive server will block indefinitely, consuming a goroutine, memory, and a file descriptor. In production, every outgoing request must have a timeout. Go provides timeouts at multiple layers: the overall client timeout, transport-level dial/TLS timeouts, and per-request context deadlines.

## Step 1 -- Set a Client-Level Timeout

```bash
mkdir -p ~/go-exercises/client-timeouts
cd ~/go-exercises/client-timeouts
go mod init client-timeouts
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get("https://httpbin.org/delay/2")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Println("Status:", resp.Status)
	fmt.Println("Body length:", len(body))
}
```

The 5-second timeout covers the entire request lifecycle: DNS, connect, TLS, send, and read.

### Intermediate Verification

```bash
go run main.go
```

Expected: Status 200 OK (the 2-second delay is within the 5-second timeout).

Now change the URL to `https://httpbin.org/delay/10`:

```bash
go run main.go
```

Expected: Error containing `context deadline exceeded` or `Client.Timeout exceeded`.

## Step 2 -- Transport-Level Timeouts

Configure timeouts for individual phases of the connection:

```go
package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

func main() {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,  // TCP connect timeout
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	resp, err := client.Get("https://httpbin.org/get")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Println("Status:", resp.Status)
	fmt.Println("Body length:", len(body))
}
```

- `DialContext Timeout`: How long to wait for TCP connection
- `TLSHandshakeTimeout`: How long for the TLS handshake
- `ResponseHeaderTimeout`: How long to wait for response headers after sending the request
- `IdleConnTimeout`: How long idle connections stay in the pool

### Intermediate Verification

```bash
go run main.go
```

Expected: Status 200 OK with the response body.

## Step 3 -- Per-Request Timeout with Context

Use `context.WithTimeout` for per-request deadlines that override the client timeout:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

func fetchWithTimeout(client *http.Client, url string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		fmt.Printf("[%s] request error: %v\n", url, err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[%s] fetch error: %v\n", url, err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("[%s] status=%d body=%d bytes\n", url, resp.StatusCode, len(body))
}

func main() {
	client := &http.Client{
		Timeout: 30 * time.Second, // generous overall timeout
	}

	// Short timeout -- will fail for slow endpoint
	fetchWithTimeout(client, "https://httpbin.org/delay/1", 3*time.Second)
	fetchWithTimeout(client, "https://httpbin.org/delay/5", 3*time.Second)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: First request succeeds (1s delay < 3s timeout). Second request fails with `context deadline exceeded`.

## Step 4 -- Detect Timeout Errors

Distinguish between timeout errors and other failures:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

func classifyError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "context deadline exceeded"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "network timeout"
	}
	return "other error"
}

func main() {
	client := &http.Client{Timeout: 2 * time.Second}

	_, err := client.Get("https://httpbin.org/delay/5")
	if err != nil {
		fmt.Printf("Error type: %s\n", classifyError(err))
		fmt.Printf("Error: %v\n", err)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: `Error type: network timeout` (or `context deadline exceeded` depending on which fires first).

## Common Mistakes

### No Timeout at All

**Wrong:**

```go
client := &http.Client{}
```

**What happens:** Requests can hang forever, leaking goroutines and connections.

**Fix:** Always set `Timeout` or use `context.WithTimeout`.

### Timeout Too Aggressive

Setting a 100ms timeout for an API that regularly takes 200ms causes constant failures. Profile your dependencies and set timeouts above the p99 latency.

### Not Canceling the Context

**Wrong:**

```go
ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
```

**What happens:** The timer goroutine leaks until the timeout expires.

**Fix:** Always `defer cancel()`.

## Verify What You Learned

Run the examples and confirm:

1. Client-level timeout blocks requests that exceed the limit
2. Transport-level timeouts catch connection phase issues
3. Context timeouts work per-request independent of the client timeout
4. Timeout errors can be distinguished from other errors

## What's Next

Continue to [07 - Cookie and Session Management](../07-cookie-and-session-management/07-cookie-and-session-management.md) to learn how to handle cookies and sessions.

## Summary

- Set `http.Client.Timeout` for an overall request deadline
- Configure `http.Transport` for per-phase timeouts (dial, TLS, response headers)
- Use `context.WithTimeout` for per-request deadlines
- Always `defer cancel()` when creating a context with timeout
- Use `errors.Is` and `errors.As` to classify timeout errors

## Reference

- [http.Client](https://pkg.go.dev/net/http#Client)
- [http.Transport](https://pkg.go.dev/net/http#Transport)
- [The complete guide to Go net/http timeouts](https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/)
