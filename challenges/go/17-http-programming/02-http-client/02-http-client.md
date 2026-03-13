# 2. HTTP Client

<!--
difficulty: basic
concepts: [http-get, http-post, response-body, io-readall, http-client]
tools: [go]
estimated_time: 20m
bloom_level: remember
prerequisites: [http-server, error-handling, io-reader]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [01 - HTTP Server with net/http](../01-http-server-with-net-http/01-http-server-with-net-http.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `http.Get` and `http.Post` to make HTTP requests
- **Read** and close a response body correctly
- **Identify** common fields of `*http.Response`

## Why HTTP Client

Go's `net/http` package includes an HTTP client as capable as its server. The default `http.Client` handles redirects, cookies, and connection pooling. For simple requests, the package-level functions `http.Get` and `http.Post` are convenient wrappers. For production code, you create a custom `http.Client` with timeouts.

## Step 1 -- Make a GET Request

```bash
mkdir -p ~/go-exercises/http-client
cd ~/go-exercises/http-client
go mod init http-client
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"io"
	"net/http"
)

func main() {
	resp, err := http.Get("https://httpbin.org/get")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()

	fmt.Println("Status:", resp.Status)
	fmt.Println("Status Code:", resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Read error:", err)
		return
	}
	fmt.Println("Body length:", len(body))
	fmt.Println(string(body[:200]))
}
```

The response body is an `io.ReadCloser` -- you must close it to release the underlying connection.

### Intermediate Verification

```bash
go run main.go
```

Expected: Status 200 OK and the JSON body from httpbin showing your request details.

## Step 2 -- Make a POST Request with JSON Body

```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func main() {
	payload := map[string]string{
		"name":  "Gopher",
		"email": "gopher@example.com",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		fmt.Println("Marshal error:", err)
		return
	}

	resp, err := http.Post(
		"https://httpbin.org/post",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()

	fmt.Println("Status:", resp.Status)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Read error:", err)
		return
	}
	fmt.Println(string(body))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: Status 200 OK. The response body shows your JSON payload in the `"json"` field.

## Step 3 -- Use http.NewRequest for Custom Headers

For full control over the request, build it manually:

```go
package main

import (
	"fmt"
	"io"
	"net/http"
)

func main() {
	req, err := http.NewRequest("GET", "https://httpbin.org/headers", nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Custom-Header", "my-value")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Read error:", err)
		return
	}
	fmt.Println(string(body))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: JSON response showing your custom headers, including `X-Custom-Header: my-value`.

## Step 4 -- Check Response Status Codes

```go
package main

import (
	"fmt"
	"net/http"
)

func fetchURL(url string) {
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Error fetching %s: %v\n", url, err)
		return
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		fmt.Printf("%s -> OK (%d)\n", url, resp.StatusCode)
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		fmt.Printf("%s -> Client Error (%d)\n", url, resp.StatusCode)
	case resp.StatusCode >= 500:
		fmt.Printf("%s -> Server Error (%d)\n", url, resp.StatusCode)
	default:
		fmt.Printf("%s -> %d\n", url, resp.StatusCode)
	}
}

func main() {
	fetchURL("https://httpbin.org/status/200")
	fetchURL("https://httpbin.org/status/404")
	fetchURL("https://httpbin.org/status/500")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
https://httpbin.org/status/200 -> OK (200)
https://httpbin.org/status/404 -> Client Error (404)
https://httpbin.org/status/500 -> Server Error (500)
```

## Common Mistakes

### Not Closing the Response Body

**Wrong:**

```go
resp, err := http.Get(url)
// forgetting defer resp.Body.Close()
```

**What happens:** Connection leak. The underlying TCP connection cannot be reused or returned to the pool.

**Fix:** Always `defer resp.Body.Close()` immediately after checking the error.

### Not Reading the Body Before Closing

**Wrong:**

```go
resp, err := http.Get(url)
defer resp.Body.Close()
// never read the body
```

**What happens:** The connection may not be reusable. The body must be fully read and drained for connection reuse.

**Fix:** Read the body with `io.ReadAll` or `io.Copy(io.Discard, resp.Body)`.

### Checking Error Before Nil Check on Response

**Wrong:**

```go
resp, err := http.Get(url)
defer resp.Body.Close() // resp may be nil if err != nil
```

**What happens:** Panic if `err != nil` because `resp` is `nil`.

**Fix:** Check `err` first, then defer the close.

## Verify What You Learned

Run all examples and confirm:

1. GET requests return the expected body
2. POST requests send JSON and receive it back
3. Custom headers appear in the response
4. Status codes are correctly categorized

## What's Next

Continue to [03 - ServeMux Routing and Patterns](../03-servemux-routing-and-patterns/03-servemux-routing-and-patterns.md) to learn Go 1.22's enhanced routing patterns.

## Summary

- `http.Get(url)` makes a simple GET request
- `http.Post(url, contentType, body)` makes a POST with a body
- `http.NewRequest` gives full control over method, headers, and body
- Always close `resp.Body` after checking the error
- Read the response body fully before closing for connection reuse
- `resp.StatusCode` is an `int` you can compare against `http.StatusOK`, etc.

## Reference

- [net/http Client documentation](https://pkg.go.dev/net/http#Client)
- [http.Get](https://pkg.go.dev/net/http#Get)
- [http.NewRequest](https://pkg.go.dev/net/http#NewRequest)
