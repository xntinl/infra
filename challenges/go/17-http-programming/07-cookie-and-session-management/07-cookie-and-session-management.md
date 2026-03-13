# 7. Cookie and Session Management

<!--
difficulty: intermediate
concepts: [http-cookie, set-cookie, cookie-attributes, session-management, secure-cookies, same-site]
tools: [go, curl]
estimated_time: 25m
bloom_level: apply
prerequisites: [http-server, middleware-chains, request-body-parsing]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [06 - HTTP Client Timeouts](../06-http-client-timeouts/06-http-client-timeouts.md)
- Understanding of HTTP request/response cycle

## Learning Objectives

After completing this exercise, you will be able to:

- **Set** cookies on HTTP responses with appropriate attributes
- **Read** cookies from incoming HTTP requests
- **Implement** a simple session store backed by an in-memory map
- **Apply** security attributes (`Secure`, `HttpOnly`, `SameSite`) to cookies

## Why Cookies and Sessions

HTTP is stateless. Cookies are the mechanism browsers use to persist state between requests. A server sets a cookie with `Set-Cookie`, and the browser sends it back on subsequent requests. Sessions build on cookies by storing a random session ID in the cookie and keeping the actual data server-side. This avoids putting sensitive data in the cookie itself and allows server-side invalidation.

## Step 1 -- Set and Read a Cookie

```bash
mkdir -p ~/go-exercises/cookies
cd ~/go-exercises/cookies
go mod init cookies
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"net/http"
	"time"
)

func setCookieHandler(w http.ResponseWriter, r *http.Request) {
	cookie := &http.Cookie{
		Name:     "username",
		Value:    "gopher",
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
		Secure:   false, // set true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, cookie)
	fmt.Fprintln(w, "Cookie set!")
}

func readCookieHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("username")
	if err != nil {
		http.Error(w, "No cookie found", http.StatusBadRequest)
		return
	}
	fmt.Fprintf(w, "Hello, %s!\n", cookie.Value)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /set", setCookieHandler)
	mux.HandleFunc("GET /read", readCookieHandler)

	fmt.Println("Server starting on :8080")
	http.ListenAndServe(":8080", mux)
}
```

`http.SetCookie` writes the `Set-Cookie` header. `r.Cookie` reads a named cookie from the request.

### Intermediate Verification

```bash
curl -v http://localhost:8080/set
```

Expected: Response includes a `Set-Cookie: username=gopher; ...` header.

```bash
curl -b "username=gopher" http://localhost:8080/read
```

Expected: `Hello, gopher!`

## Step 2 -- Delete a Cookie

Add a handler that expires the cookie:

```go
func deleteCookieHandler(w http.ResponseWriter, r *http.Request) {
	cookie := &http.Cookie{
		Name:   "username",
		Value:  "",
		Path:   "/",
		MaxAge: -1, // negative MaxAge deletes the cookie
	}
	http.SetCookie(w, cookie)
	fmt.Fprintln(w, "Cookie deleted!")
}
```

Register it in `main`:

```go
mux.HandleFunc("GET /delete", deleteCookieHandler)
```

### Intermediate Verification

```bash
curl -v http://localhost:8080/delete
```

Expected: `Set-Cookie` header with `Max-Age=0` or a past `Expires` date.

## Step 3 -- Build a Session Store

Create an in-memory session store that maps session IDs to user data:

```go
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
)

type Session struct {
	Username string
	Data     map[string]string
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
	}
}

func (s *SessionStore) Create(username string) (string, *Session) {
	id := generateSessionID()
	session := &Session{
		Username: username,
		Data:     make(map[string]string),
	}
	s.mu.Lock()
	s.sessions[id] = session
	s.mu.Unlock()
	return id, session
}

func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return session, ok
}

func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

func generateSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
```

### Intermediate Verification

This is a building block. Verify it compiles:

```bash
go build ./...
```

## Step 4 -- Wire Sessions to HTTP Handlers

Add login, profile, and logout handlers:

```go
var store = NewSessionStore()

func loginHandler(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("user")
	if username == "" {
		http.Error(w, "user query param required", http.StatusBadRequest)
		return
	}

	sessionID, _ := store.Create(username)
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	fmt.Fprintf(w, "Logged in as %s\n", username)
}

func profileHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		http.Error(w, "not logged in", http.StatusUnauthorized)
		return
	}

	session, ok := store.Get(cookie.Value)
	if !ok {
		http.Error(w, "session expired", http.StatusUnauthorized)
		return
	}

	fmt.Fprintf(w, "Welcome back, %s!\n", session.Username)
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err == nil {
		store.Delete(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   "session_id",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	fmt.Fprintln(w, "Logged out")
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", loginHandler)
	mux.HandleFunc("GET /profile", profileHandler)
	mux.HandleFunc("GET /logout", logoutHandler)

	fmt.Println("Server starting on :8080")
	http.ListenAndServe(":8080", mux)
}
```

### Intermediate Verification

```bash
# Login and capture the cookie
curl -c cookies.txt http://localhost:8080/login?user=alice

# Use the session cookie
curl -b cookies.txt http://localhost:8080/profile

# Logout
curl -b cookies.txt http://localhost:8080/logout

# Verify session is gone
curl -b cookies.txt http://localhost:8080/profile
```

Expected: Login succeeds, profile shows the username, logout clears the session, and the final profile request returns `session expired`.

## Common Mistakes

### Storing Sensitive Data in the Cookie Value

**Wrong:** Putting the username or role directly in the cookie.

**What happens:** Users can modify cookie values to impersonate others.

**Fix:** Store only a random session ID in the cookie. Keep real data server-side.

### Forgetting HttpOnly

**Wrong:** Setting `HttpOnly: false` on session cookies.

**What happens:** JavaScript can read the cookie via `document.cookie`, enabling XSS-based session theft.

**Fix:** Always set `HttpOnly: true` for session cookies.

### Using Predictable Session IDs

**Wrong:** Using an incrementing counter or timestamp as session ID.

**What happens:** Attackers can guess valid session IDs.

**Fix:** Use `crypto/rand` to generate session IDs with at least 128 bits of entropy.

## Verify What You Learned

Run through the full login/profile/logout flow and confirm:

1. The `Set-Cookie` header includes `HttpOnly` and `SameSite`
2. The profile endpoint rejects requests without a valid session
3. Logging out invalidates the session on the server side

## What's Next

Continue to [08 - File Upload and Multipart Forms](../08-file-upload-and-multipart-forms/08-file-upload-and-multipart-forms.md) to learn how to handle file uploads.

## Summary

- `http.SetCookie` writes a `Set-Cookie` header; `r.Cookie` reads a cookie from the request
- Set `MaxAge: -1` to delete a cookie
- Session management stores a random ID in the cookie and maps it to server-side data
- Always use `HttpOnly`, `Secure` (in production), and `SameSite` attributes
- Use `crypto/rand` for session ID generation

## Reference

- [http.SetCookie](https://pkg.go.dev/net/http#SetCookie)
- [http.Cookie](https://pkg.go.dev/net/http#Cookie)
- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
