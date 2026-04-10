# 32. Build a Web Framework (Phoenix-like)

**Difficulty**: Insane

---

## Prerequisites

- Raw TCP socket servers with `:gen_tcp`
- HTTP/1.1 specification (RFC 7230–7235)
- Elixir macros for DSL construction
- WebSocket handshake (RFC 6455)
- HMAC-based message authentication
- EEx template engine basics
- Plug specification and middleware chains

---

## Problem Statement

Build a full-stack web framework from the TCP socket layer up. The framework must handle raw HTTP/1.1 connections, expose a declarative routing and controller DSL, support a middleware pipeline, render EEx templates, upgrade connections to WebSockets, and handle sessions with cryptographic signing. An application built with this framework must not depend on Phoenix or Plug — only on Elixir standard library and OTP.

1. Implement an HTTP/1.1 parser that correctly handles request line, headers, chunked and content-length body framing
2. Build a declarative router macro DSL that generates efficient pattern-match dispatch at compile time
3. Define a middleware (plug) pipeline abstraction where each middleware can halt or pass to the next layer
4. Render EEx templates with layouts and partials without depending on the Phoenix template engine
5. Upgrade HTTP connections to WebSockets and multiplex them over named channels
6. Manage cookie-based sessions signed with HMAC-SHA256 to prevent tampering
7. Serve static files from a configured directory with proper Content-Type, ETag, and caching headers

---

## Acceptance Criteria

- [ ] HTTP/1.1 parser: parses request line (method, path, version), headers (case-insensitive), and body; supports `Content-Length` and `Transfer-Encoding: chunked`; handles `keep-alive` connections; returns a structured request struct with method, path, query params, headers, and body
- [ ] Router: `get "/users/:id", UserController, :show` registers a route at compile time; path parameters (`:name`) are extracted and merged into `params`; catch-all `match` and scoped `scope "/api"` grouping; compiled dispatch is a single function with pattern-match clauses
- [ ] Controller: a controller module defines action functions with `(conn, params)` signature; `conn` is a struct carrying request, response headers, status, and assigns; `send_resp(conn, 200, body)` and `json(conn, map)` are available; halting the pipeline is explicit
- [ ] Plug pipeline: `plug Authenticate` inserts middleware into the pipeline; each plug is a module with `call(conn, opts)` returning a (possibly modified) conn; a plug can `halt(conn)` to stop further processing; plugs compose with `|>` semantics
- [ ] Views/Templates: `render(conn, "index.html", assigns)` evaluates `templates/controller_name/index.html.eex` with the assigns; a layout wraps the inner template; partials are rendered via `render_partial("_header.html.eex", assigns)`; template compilation is done at application start, not per request
- [ ] WebSockets: `GET /ws` with the correct upgrade headers is upgraded to a WebSocket connection (RFC 6455 handshake including Sec-WebSocket-Accept calculation); the connection is moved to a channel process; `channel "room:lobby", RoomChannel` routes messages by topic; channels receive `handle_in/3` and can `push/3` to connected clients
- [ ] Session: sessions are stored in a signed cookie; the signature is `HMAC-SHA256(secret_key, cookie_value)`; tampering or expiry causes the session to be treated as empty; `put_session/3` and `get_session/2` are available in controllers
- [ ] JSON API: `json(conn, %{users: [...]})` sets `Content-Type: application/json` and encodes the map with `Jason` (or a self-built encoder); `get_body_as_json(conn)` decodes and validates a JSON request body
- [ ] Error handling: uncaught exceptions in controllers return a 500 response; undefined routes return a 404; both support custom error view modules; errors are logged with request context
- [ ] Static files: `plug Static, at: "/static", from: "priv/static"` serves files; sets `Content-Type` from extension; sends `ETag` based on file content hash; responds with `304 Not Modified` when `If-None-Match` matches

---

## What You Will Learn

- HTTP/1.1 wire protocol: framing, keep-alive, chunked encoding
- Compile-time route compilation using macros for O(1) dispatch
- Middleware pipeline as a function composition pattern
- WebSocket handshake mechanics (SHA-1 key derivation, frame masking)
- Cookie signing and the importance of constant-time comparison
- Template compilation vs. interpretation trade-offs
- How Phoenix channels work conceptually (topic-based pub/sub over WebSocket)

---

## Hints

- Read RFC 7230 (HTTP/1.1 Message Syntax) and RFC 6455 (WebSockets) before implementing those layers
- Study how Plug defines its `conn` struct — it is a simple map with well-known keys
- Investigate how Phoenix Router compiles routes to `match/5` clauses using macros — your approach should be similar
- Research constant-time binary comparison (`Plug.Crypto.secure_compare/2`) and why it matters for HMAC verification
- Think about how template compilation works: `EEx.compile_file/2` generates Elixir AST at compile time
- Look into how the WebSocket frame format encodes opcode, masking, and payload length in a compact binary header

---

## Reference Material

- RFC 7230 — HTTP/1.1 Message Syntax and Routing
- RFC 6455 — The WebSocket Protocol
- Phoenix Framework source code (github.com/phoenixframework/phoenix)
- Plug specification (hex.pm/packages/plug)
- "Programming Phoenix 1.4" — Chris McCord, Bruce Tate, Jose Valim

---

## Difficulty Rating ★★★★★★

Building every layer from raw TCP to WebSocket channels, template rendering, and cryptographically signed sessions without any framework dependency is the most comprehensive single exercise in this curriculum.

---

## Estimated Time

100–160 hours
