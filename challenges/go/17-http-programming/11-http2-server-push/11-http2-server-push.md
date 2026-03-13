# 11. HTTP/2 Server Push

<!--
difficulty: advanced
concepts: [http2, server-push, tls, http-pusher, h2, multiplexing]
tools: [go, curl, openssl]
estimated_time: 35m
bloom_level: analyze
prerequisites: [http-server, tls, middleware-chains]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of TLS certificates (self-signed is fine)
- Familiarity with HTTP server patterns

## Learning Objectives

After completing this exercise, you will be able to:

- **Configure** a Go HTTP/2 server with TLS
- **Use** `http.Pusher` to push resources before the client requests them
- **Verify** HTTP/2 connections and pushed streams with curl
- **Explain** when server push helps and when it hurts performance

## Why HTTP/2 Server Push

HTTP/2 multiplexes multiple streams over a single TCP connection. Server push takes this further: the server can proactively send resources (CSS, JS, images) it knows the client will need, before the client even parses the HTML. This eliminates one round-trip per pushed resource. Go's `net/http` supports HTTP/2 automatically when TLS is enabled and exposes push via the `http.Pusher` interface.

Note: HTTP/2 server push has been removed from Chrome and some other browsers as of 2022 due to limited real-world gains. This exercise teaches the API and the underlying concepts, which remain relevant for understanding HTTP/2 multiplexing and gRPC streams.

## The Problem

Build an HTTP/2 server that serves an HTML page and pushes associated CSS and JavaScript files before the browser requests them. Observe the difference in waterfall timing.

## Requirements

1. Generate a self-signed TLS certificate for local development
2. Serve an HTML page that references CSS and JS files
3. Use `http.Pusher` to push the CSS and JS alongside the HTML response
4. Verify pushed streams using `curl --http2`

## Step 1 -- Generate a Self-Signed Certificate

```bash
mkdir -p ~/go-exercises/http2-push
cd ~/go-exercises/http2-push
go mod init http2-push

openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout server.key -out server.crt \
  -days 365 -subj "/CN=localhost"
```

### Intermediate Verification

```bash
ls server.crt server.key
```

Expected: Both files exist.

## Step 2 -- Create an HTTP/2 Server with Push

Create `main.go`:

```go
package main

import (
	"fmt"
	"log"
	"net/http"
)

const htmlContent = `<!DOCTYPE html>
<html>
<head>
    <link rel="stylesheet" href="/static/style.css">
</head>
<body>
    <h1>HTTP/2 Server Push Demo</h1>
    <p id="message">Loading...</p>
    <script src="/static/app.js"></script>
</body>
</html>`

const cssContent = `body {
    font-family: sans-serif;
    margin: 40px;
    background: #f5f5f5;
}
h1 { color: #333; }`

const jsContent = `document.getElementById('message').textContent = 'Loaded with HTTP/2!';`

func indexHandler(w http.ResponseWriter, r *http.Request) {
	// Attempt to push associated resources
	if pusher, ok := w.(http.Pusher); ok {
		targets := []string{"/static/style.css", "/static/app.js"}
		for _, target := range targets {
			if err := pusher.Push(target, nil); err != nil {
				log.Printf("Push failed for %s: %v", target, err)
			} else {
				log.Printf("Pushed %s", target)
			}
		}
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlContent)
}

func cssHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css")
	fmt.Fprint(w, cssContent)
}

func jsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	fmt.Fprint(w, jsContent)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", indexHandler)
	mux.HandleFunc("GET /static/style.css", cssHandler)
	mux.HandleFunc("GET /static/app.js", jsHandler)

	fmt.Println("HTTP/2 server on https://localhost:8443")
	log.Fatal(http.ListenAndServeTLS(":8443", "server.crt", "server.key", mux))
}
```

Go automatically enables HTTP/2 when using `ListenAndServeTLS`. The `http.Pusher` interface is available when the connection is HTTP/2.

### Intermediate Verification

```bash
go run main.go &
curl -k --http2 -v https://localhost:8443/
```

Expected: The `-v` output shows `< HTTP/2 200` and the HTML content. The server log shows "Pushed /static/style.css" and "Pushed /static/app.js".

## Step 3 -- Verify Push Streams

```bash
curl -k --http2 -v https://localhost:8443/ 2>&1 | grep -i "http/2"
```

Expected output includes:

```
* using HTTP/2
< HTTP/2 200
```

The pushed resources are sent proactively. To observe them, use a browser with HTTP/2 developer tools or `nghttp`:

```bash
# If nghttp2 is installed:
nghttp -v https://localhost:8443/
```

### Intermediate Verification

```bash
# Verify the individual resources are still servable
curl -k https://localhost:8443/static/style.css
curl -k https://localhost:8443/static/app.js
```

Expected: CSS and JS content respectively.

## Step 4 -- Conditional Push

Only push when the client does not already have cached resources:

```go
func indexHandler(w http.ResponseWriter, r *http.Request) {
	pusher, ok := w.(http.Pusher)
	if !ok {
		log.Println("Push not supported (HTTP/1.1 connection)")
	}

	// Only push if client has no cookie indicating cached resources
	_, err := r.Cookie("resources_cached")
	if err != nil && pusher != nil {
		targets := []string{"/static/style.css", "/static/app.js"}
		for _, target := range targets {
			if err := pusher.Push(target, nil); err != nil {
				log.Printf("Push failed: %v", err)
			}
		}

		// Set cookie so we skip push on next visit
		http.SetCookie(w, &http.Cookie{
			Name:   "resources_cached",
			Value:  "1",
			MaxAge: 3600,
			Path:   "/",
		})
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlContent)
}
```

### Intermediate Verification

```bash
# First request: pushes resources
curl -k -c cookies.txt --http2 https://localhost:8443/

# Second request with cookie: skips push
curl -k -b cookies.txt --http2 https://localhost:8443/
```

Expected: Server logs show pushes only on the first request.

## Hints

- `http.Pusher` is only available on HTTP/2 connections -- always check with a type assertion
- Push must happen before the response body is written (or at least before it is flushed)
- The `nil` second argument to `Push` means use default push options; you can pass `&http.PushOptions{Header: h}` to customize headers
- Go enables HTTP/2 automatically with TLS; for plaintext HTTP/2 (h2c), you need `golang.org/x/net/http2/h2c`
- Server push is deprecated in most browsers, but the API still works for non-browser clients and gRPC

## Verification

- Server responds with HTTP/2 protocol (visible in `curl -v` output)
- Server logs confirm push attempts for CSS and JS
- Individual static resources are still accessible via direct requests
- Conditional push avoids redundant pushes for repeat visitors
- Falling back to HTTP/1.1 does not cause errors (push is simply skipped)

## What's Next

Continue to [12 - Reverse Proxy and Load Balancer](../12-reverse-proxy-and-load-balancer/12-reverse-proxy-and-load-balancer.md) to learn how to build a reverse proxy in Go.

## Summary

HTTP/2 is enabled automatically in Go when using TLS. The `http.Pusher` interface lets the server push resources before the client requests them. Always check for pusher support with a type assertion. Push before writing the response body. While browser support for server push has waned, the concepts are important for understanding HTTP/2 multiplexing and server-initiated streams.

## Reference

- [http.Pusher](https://pkg.go.dev/net/http#Pusher)
- [HTTP/2 in Go](https://pkg.go.dev/golang.org/x/net/http2)
- [HTTP/2 Server Push](https://en.wikipedia.org/wiki/HTTP/2_Server_Push)
- [http.ListenAndServeTLS](https://pkg.go.dev/net/http#ListenAndServeTLS)
