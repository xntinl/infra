# Web Framework from TCP to WebSockets

**Project**: `nova` — a full-stack web framework with zero Phoenix or Plug dependency

---

## Project context

You are building `nova`, a web framework that the platform team will use for internal tooling services where Phoenix is considered too heavy a dependency. It starts from a raw TCP socket and builds up: HTTP/1.1 parser, router DSL, controller layer, template rendering, WebSocket channels, signed sessions, and static file serving — all without depending on Phoenix, Plug, or Cowboy.

Project structure:

```
nova/
├── lib/
│   └── nova/
│       ├── application.ex
│       ├── transport/
│       │   ├── tcp_server.ex        # ← :gen_tcp listener + connection pool
│       │   └── http_parser.ex       # ← HTTP/1.1 request line + headers + body
│       ├── conn.ex                  # ← request/response struct (the "conn")
│       ├── router.ex                # ← macro DSL → compile-time pattern dispatch
│       ├── controller.ex            # ← action conventions + response helpers
│       ├── plug.ex                  # ← middleware behaviour + pipeline
│       ├── template/
│       │   ├── engine.ex            # ← EEx compilation + layout + partials
│       │   └── view.ex              # ← render/3 helper
│       ├── websocket/
│       │   ├── handshake.ex         # ← RFC 6455 upgrade + Sec-WebSocket-Accept
│       │   ├── frame.ex             # ← frame encode/decode (opcode, mask, payload)
│       │   └── channel.ex           # ← topic routing + handle_in/push
│       ├── session.ex               # ← HMAC-SHA256 signed cookies
│       └── static.ex                # ← file serving + ETag + 304
├── test/
│   └── nova/
│       ├── http_parser_test.exs
│       ├── router_test.exs
│       ├── plug_test.exs
│       ├── websocket_test.exs
│       └── session_test.exs
├── bench/
│   └── router_bench.exs
└── mix.exs
```

---

## The business problem

The platform team maintains 12 internal tools. Each depends on Phoenix, which brings Ecto, Telemetry, Cowboy, and 30 other transitive dependencies. A vulnerability in any of them triggers an upgrade across all 12 services. Nova must run with only the Elixir standard library — any production tool built on it will have a minimal, auditable dependency tree.

Three invariants drive the design:

1. **Zero-dependency core** — `nova` must start with only `mix new nova`.
2. **Compile-time routes** — path dispatch must be a pattern-match clause, not a runtime map lookup.
3. **No `eval` in production** — templates compile to Elixir functions at application start.

---

## Why compile-time route dispatch matters

A runtime router stores routes in a map and performs string matching per request:

```
GET /users/42 → Map.get(routes, "/users/:id") → :not_found? do prefix match?...
```

This is O(n) in the number of routes for naive implementations. A macro-generated router compiles each route to a function clause:

```elixir
def match("GET", ["users", id], conn), do: UserController.show(conn, %{id: id})
def match("GET", ["users"], conn),     do: UserController.index(conn, %{})
def match(_, _, conn),                 do: send_resp(conn, 404, "Not Found")
```

BEAM's pattern matching dispatches in O(1) — the compiler generates a hash-based jump table for function clause dispatch. A router with 1000 routes performs identically to one with 10 routes.

---

## Why WebSocket key derivation uses SHA-1 (and why that's fine)

The `Sec-WebSocket-Accept` header is computed as:

```
Base64(SHA-1(client_key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
```

SHA-1 is considered cryptographically broken for general use but is safe here: the magic GUID makes length-extension attacks irrelevant, and the purpose is not confidentiality — it is handshake verification to prevent non-browser HTTP clients from accidentally connecting to WebSocket endpoints. TLS provides the actual confidentiality.

---

## Implementation

### Step 1: Create the project

```bash
mix new nova --sup
cd nova
mkdir -p lib/nova/{transport,template,websocket}
mkdir -p test/nova bench
```

### Step 2: `mix.exs`

```elixir
defp deps do
  [
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/nova/transport/http_parser.ex`

```elixir
defmodule Nova.Transport.HttpParser do
  @moduledoc """
  Parses HTTP/1.1 requests from raw binary data.

  Wire format:
    METHOD SP request-target SP HTTP/version CRLF
    *(header-field CRLF)
    CRLF
    message-body

  The parser is a state machine: :request_line → :headers → :body → :done.
  This matters for keep-alive: after one request is fully parsed, the socket
  remains open and the next request begins from :request_line again.
  """

  defstruct [:method, :path, :query, :version, :headers, :body, state: :request_line]

  @crlf "\r\n"

  @doc """
  Parses raw bytes into a request struct.
  Returns {:ok, request, remaining_bytes} or {:error, reason}.
  Remaining bytes may be the start of the next pipelined request.
  """
  @spec parse(binary()) :: {:ok, t(), binary()} | {:error, atom()} | :incomplete
  def parse(data) do
    with {:ok, {method, path, query, version}, rest} <- parse_request_line(data),
         {:ok, headers, rest} <- parse_headers(rest),
         {:ok, body, rest} <- parse_body(rest, headers) do
      req = %__MODULE__{
        method: method,
        path: path,
        query: query,
        version: version,
        headers: headers,
        body: body,
        state: :done
      }
      {:ok, req, rest}
    end
  end

  defp parse_request_line(data) do
    case :binary.split(data, @crlf) do
      [line, rest] ->
        # TODO: parse "METHOD /path?query HTTP/1.1"
        # HINT: String.split(line, " ") gives [method, target, version]
        # HINT: URI.parse/1 splits path and query
        {:error, :not_implemented}

      [_incomplete] ->
        :incomplete
    end
  end

  defp parse_headers(data) do
    # TODO: split on "\r\n\r\n" to find header/body boundary
    # TODO: parse each "Name: value\r\n" pair into a list of {name, value} tuples
    # TODO: header names are case-insensitive — downcase them
    # HINT: :binary.split(data, "\r\n\r\n") gives [headers_block, body_start]
    {:error, :not_implemented}
  end

  defp parse_body(data, headers) do
    content_length = get_header(headers, "content-length")
    transfer_encoding = get_header(headers, "transfer-encoding")

    cond do
      transfer_encoding == "chunked" ->
        # TODO: parse chunked encoding: hex_size\r\ndata\r\n ... 0\r\n\r\n
        {:error, :not_implemented}

      content_length != nil ->
        len = String.to_integer(content_length)
        # TODO: verify we have `len` bytes available; if not, return :incomplete
        {:error, :not_implemented}

      true ->
        {:ok, "", data}
    end
  end

  defp get_header(headers, name) do
    case List.keyfind(headers, name, 0) do
      {_, value} -> value
      nil -> nil
    end
  end
end
```

### Step 4: `lib/nova/router.ex`

```elixir
defmodule Nova.Router do
  @moduledoc """
  Macro DSL for compile-time route dispatch.

  Usage:
    defmodule MyApp.Router do
      use Nova.Router

      scope "/api" do
        get "/users", UserController, :index
        get "/users/:id", UserController, :show
        post "/users", UserController, :create
      end

      get "/", HomeController, :index
    end

  At compile time, __before_compile__ generates:

    def match("GET", ["api", "users"], conn), do: UserController.index(conn, %{})
    def match("GET", ["api", "users", id], conn), do: UserController.show(conn, %{"id" => id})
    def match(_, _, conn), do: send_resp(conn, 404, "Not Found")

  The path is split into segments at compile time — matching is purely structural.
  """

  defmacro __using__(_) do
    quote do
      import Nova.Router
      Module.register_attribute(__MODULE__, :nova_routes, accumulate: true)
      Module.register_attribute(__MODULE__, :nova_scope_prefix, [])
      @before_compile Nova.Router
    end
  end

  defmacro __before_compile__(env) do
    routes = Module.get_attribute(env.module, :nova_routes) |> Enum.reverse()

    # Generate one match/3 clause per route
    clauses = Enum.map(routes, fn {method, path, controller, action} ->
      {segments, param_names} = compile_path(path)
      # TODO: generate a function clause that:
      #   1. pattern-matches on method and segments
      #   2. extracts path params from the pattern
      #   3. calls controller.action(conn, params)
    end)

    # Catch-all 404 clause
    catch_all = quote do
      def match(_method, _path, conn) do
        Nova.Conn.send_resp(conn, 404, "Not Found")
      end
    end

    quote do
      unquote_splicing(clauses)
      unquote(catch_all)
    end
  end

  defmacro get(path, controller, action) do
    quote do
      @nova_routes {"GET", Path.join(@nova_scope_prefix || "", unquote(path)), unquote(controller), unquote(action)}
    end
  end

  defmacro post(path, controller, action) do
    quote do
      @nova_routes {"POST", Path.join(@nova_scope_prefix || "", unquote(path)), unquote(controller), unquote(action)}
    end
  end

  # TODO: add put/2, delete/2, patch/2

  defmacro scope(prefix, do: block) do
    quote do
      old_prefix = @nova_scope_prefix
      @nova_scope_prefix unquote(prefix)
      unquote(block)
      @nova_scope_prefix old_prefix
    end
  end

  defp compile_path(path) do
    segments = String.split(path, "/", trim: true)
    # TODO: for each segment:
    #   - literal "users" → quoted string "users"
    #   - param ":id" → a quoted variable `id` captured in params
    # Return {pattern_segments, param_names}
    {segments, []}
  end
end
```

### Step 5: `lib/nova/websocket/handshake.ex`

```elixir
defmodule Nova.WebSocket.Handshake do
  @moduledoc """
  Implements the RFC 6455 WebSocket opening handshake.

  The upgrade flow:
  1. Client sends HTTP GET with:
     Upgrade: websocket
     Connection: Upgrade
     Sec-WebSocket-Key: <base64 of 16 random bytes>
     Sec-WebSocket-Version: 13

  2. Server responds with HTTP 101:
     HTTP/1.1 101 Switching Protocols
     Upgrade: websocket
     Connection: Upgrade
     Sec-WebSocket-Accept: <derived key>

  The derived key prevents non-WebSocket HTTP clients from accidentally connecting:
    accept = Base64(SHA-1(client_key <> magic_guid))
  """

  @magic_guid "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

  @doc """
  Returns true if this HTTP request is a WebSocket upgrade request.
  """
  @spec upgrade_request?(Nova.Conn.t()) :: boolean()
  def upgrade_request?(conn) do
    # TODO: check Upgrade: websocket and Connection: Upgrade headers (case-insensitive)
    false
  end

  @doc """
  Builds the 101 Switching Protocols response headers.
  Returns the raw HTTP response bytes to write to the socket.
  """
  @spec build_response(String.t()) :: binary()
  def build_response(client_key) do
    accept = compute_accept(client_key)

    # TODO: build the response string with correct CRLF line endings
    # "HTTP/1.1 101 Switching Protocols\r\n" <>
    # "Upgrade: websocket\r\n" <>
    # "Connection: Upgrade\r\n" <>
    # "Sec-WebSocket-Accept: #{accept}\r\n\r\n"
  end

  @spec compute_accept(String.t()) :: String.t()
  def compute_accept(client_key) do
    # TODO: SHA-1(client_key <> @magic_guid) then Base64 encode
    # HINT: :crypto.hash(:sha, data) returns raw binary
    # HINT: Base.encode64/1 encodes to base64 string
  end
end
```

### Step 6: `lib/nova/session.ex`

```elixir
defmodule Nova.Session do
  @moduledoc """
  Cookie-based sessions signed with HMAC-SHA256.

  The cookie value is: base64(payload) <> "." <> base64(hmac)
  Tampering with the payload invalidates the HMAC and the session is treated as empty.

  Why HMAC and not encryption?
  The session payload is visible to the client (it's base64, not encrypted).
  HMAC prevents the client from forging a different payload — it cannot produce a
  valid signature without knowing the secret key.

  For confidential session data, combine HMAC with AES-GCM encryption.
  """

  @doc """
  Signs a session payload (a map) into a cookie value string.
  """
  @spec sign(map(), String.t()) :: String.t()
  def sign(payload, secret_key) do
    json = Jason.encode!(payload)
    encoded = Base.url_encode64(json, padding: false)
    mac = compute_mac(encoded, secret_key)
    "#{encoded}.#{mac}"
  end

  @doc """
  Verifies and decodes a cookie value.
  Returns {:ok, payload} or {:error, :invalid | :tampered}.
  """
  @spec verify(String.t(), String.t()) :: {:ok, map()} | {:error, atom()}
  def verify(cookie_value, secret_key) do
    with [encoded, received_mac] <- String.split(cookie_value, ".", parts: 2),
         expected_mac = compute_mac(encoded, secret_key),
         true <- secure_compare(received_mac, expected_mac),
         {:ok, json} <- Base.url_decode64(encoded, padding: false),
         {:ok, payload} <- Jason.decode(json) do
      {:ok, payload}
    else
      false -> {:error, :tampered}
      _ -> {:error, :invalid}
    end
  end

  defp compute_mac(data, secret_key) do
    # TODO: :crypto.mac(:hmac, :sha256, secret_key, data) |> Base.url_encode64(padding: false)
  end

  defp secure_compare(a, b) do
    # TODO: constant-time comparison to prevent timing attacks
    # HINT: XOR every byte, OR all results; if the final OR is 0, they are equal
    # NEVER use == for MAC comparison — it short-circuits on first mismatch
    # revealing the comparison time and enabling timing attacks
  end
end
```

### Step 7: Given tests — must pass without modification

```elixir
# test/nova/http_parser_test.exs
defmodule Nova.HttpParserTest do
  use ExUnit.Case, async: true

  alias Nova.Transport.HttpParser

  test "parses GET request with no body" do
    raw = "GET /users/42?include=posts HTTP/1.1\r\nHost: example.com\r\n\r\n"
    assert {:ok, req, ""} = HttpParser.parse(raw)
    assert req.method == "GET"
    assert req.path == "/users/42"
    assert req.query == "include=posts"
    assert List.keymember?(req.headers, "host", 0)
  end

  test "parses POST with Content-Length body" do
    body = ~s({"name":"alice"})
    raw = "POST /users HTTP/1.1\r\nContent-Length: #{byte_size(body)}\r\n\r\n#{body}"
    assert {:ok, req, ""} = HttpParser.parse(raw)
    assert req.method == "POST"
    assert req.body == body
  end

  test "returns :incomplete for partial data" do
    assert :incomplete = HttpParser.parse("GET /users HTTP/1.1\r\n")
  end

  test "headers are case-normalized to lowercase" do
    raw = "GET / HTTP/1.1\r\nContent-Type: application/json\r\n\r\n"
    assert {:ok, req, ""} = HttpParser.parse(raw)
    assert List.keymember?(req.headers, "content-type", 0)
  end
end
```

```elixir
# test/nova/session_test.exs
defmodule Nova.SessionTest do
  use ExUnit.Case, async: true

  alias Nova.Session

  @secret "test_secret_key_minimum_32_bytes_long"

  test "sign and verify round-trips" do
    payload = %{"user_id" => 42, "role" => "admin"}
    cookie = Session.sign(payload, @secret)
    assert {:ok, decoded} = Session.verify(cookie, @secret)
    assert decoded["user_id"] == 42
  end

  test "tampered payload is rejected" do
    cookie = Session.sign(%{"user_id" => 1}, @secret)
    # Modify the payload portion of the cookie
    [encoded, mac] = String.split(cookie, ".", parts: 2)
    tampered = Base.url_encode64(~s({"user_id":999}), padding: false)
    assert {:error, :tampered} = Session.verify("#{tampered}.#{mac}", @secret)
  end

  test "wrong secret is rejected" do
    cookie = Session.sign(%{"user_id" => 1}, @secret)
    assert {:error, :tampered} = Session.verify(cookie, "wrong_secret")
  end

  test "empty session returns error" do
    assert {:error, _} = Session.verify("", @secret)
  end
end
```

```elixir
# test/nova/websocket_test.exs
defmodule Nova.WebSocketTest do
  use ExUnit.Case, async: true

  alias Nova.WebSocket.Handshake

  test "computes correct Sec-WebSocket-Accept for RFC example" do
    # RFC 6455 example key
    client_key = "dGhlIHNhbXBsZSBub25jZQ=="
    expected_accept = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="

    assert Handshake.compute_accept(client_key) == expected_accept
  end

  test "build_response produces valid 101 headers" do
    response = Handshake.build_response("dGhlIHNhbXBsZSBub25jZQ==")
    assert String.contains?(response, "HTTP/1.1 101")
    assert String.contains?(response, "Upgrade: websocket")
    assert String.contains?(response, "Sec-WebSocket-Accept:")
  end
end
```

### Step 8: Run the tests

```bash
mix test test/nova/ --trace
```

### Step 9: Router benchmark

```elixir
# bench/router_bench.exs
defmodule BenchRouter do
  use Nova.Router
  for i <- 1..100 do
    get "/resource#{i}/:id", FakeController, :show
  end
end

Benchee.run(
  %{
    "route /resource50/42" => fn ->
      conn = %Nova.Conn{method: "GET", path_segments: ["resource50", "99"]}
      BenchRouter.match("GET", ["resource50", "99"], conn)
    end
  },
  time: 5,
  warmup: 2
)
```

Expected: dispatch with 100 routes should be under 1µs. If you see > 10µs, verify your `match/3` clauses are true pattern-match clauses — not a `cond` or `Enum.find` at runtime.

---

## Trade-off analysis

| Aspect | Nova (yours) | Phoenix | Plug standalone |
|--------|-------------|---------|----------------|
| Compile-time routes | yes | yes | no |
| HTTP/2 support | no | yes (Cowboy) | no |
| WebSocket channels | basic | full (presence, PubSub) | no |
| Template compilation | EEx at startup | HEEx + LiveView | no |
| Dependencies | 0 (+ Jason) | 30+ transitive | 5+ |
| TLS | not built-in | via Cowboy | via Cowboy |
| Production battle-tested | no | yes | yes |

Reflection: Phoenix compiles templates with HEEx (HTML-aware EEx). How does HEEx's compile-time HTML parsing prevent XSS vulnerabilities that standard EEx cannot? What would you need to add to Nova's template engine to offer the same guarantee?

---

## Common production mistakes

**1. Using `==` for MAC comparison**
String equality in Elixir short-circuits on the first differing byte. An attacker can measure response times to determine how many leading bytes of a forged MAC are correct. Always use constant-time comparison for cryptographic values.

**2. Generating `Sec-WebSocket-Accept` without the magic GUID**
A common mistake is Base64(SHA-1(client_key)) without appending the GUID. The resulting accept key will not match what any standard browser client expects.

**3. Not handling `keep-alive` connections**
HTTP/1.1 defaults to `Connection: keep-alive`. After parsing one request, the socket stays open. A server that closes the socket after each response forces a new TCP handshake per request. Your TCP server must loop back to parsing after sending the response.

**4. Template compilation at request time**
Calling `EEx.eval_file/2` on every request re-reads and re-compiles the template file. This adds 10–100ms per request and burns I/O. Compile with `EEx.compile_file/1` once at application start and store the compiled AST.

**5. Path segment matching with URL encoding**
A client may request `/users/alice%20smith`. Your path splitter must URL-decode each segment before matching. Failure to do so means `%20` never matches a `:name` capture for `"alice smith"`.

---

## Resources

- [RFC 7230 — HTTP/1.1 Message Syntax](https://www.rfc-editor.org/rfc/rfc7230) — sections 3–5 cover the wire format your parser must implement
- [RFC 6455 — The WebSocket Protocol](https://www.rfc-editor.org/rfc/rfc6455) — section 1.3 (opening handshake) and section 5 (framing) are the two pieces you implement
- [Phoenix Framework source](https://github.com/phoenixframework/phoenix) — study `Phoenix.Router` for the macro pattern; `Phoenix.Socket` for channel multiplexing
- [Plug specification](https://hexdocs.pm/plug/readme.html) — your `Nova.Plug` behaviour should be compatible so existing Plug middlewares can be adapted
- ["Programming Phoenix 1.4"](https://pragprog.com/titles/phoenix14/programming-phoenix-1-4/) — McCord, Tate, Valim — chapters on Router internals and Channel architecture
