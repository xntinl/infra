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

## Why pattern-matched function-head routing and not a trie or regex-based router

the BEAM's pattern matcher is already a highly optimized decision tree; compiling routes into function heads lets us reuse that engine for free. A user-space trie reimplements what the compiler gives us.

## Design decisions

**Option A — runtime route lookup via Enum.find over a list**
- Pros: trivial to implement, dynamic routes at runtime
- Cons: O(n) per request, unacceptable for >50 routes

**Option B — compile-time route compilation into pattern-matched function heads** (chosen)
- Pros: O(1) dispatch via BEAM's pattern matcher, zero allocation
- Cons: routes are fixed at compile time

→ Chose **B** because routing is on every request's hot path; O(n) scanning would dominate latency on realistic apps.

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

**Objective**: Scaffold the web framework phoenix like Mix project with the required directory layout.


```bash
mix new nova --sup
cd nova
mkdir -p lib/nova/{transport,template,websocket}
mkdir -p test/nova bench
```

### Step 2: `mix.exs`

**Objective**: Declare the Mix project configuration and third-party dependencies.


```elixir
defp deps do
  [
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

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

**Objective**: Scaffold the web framework phoenix like Mix project with the required directory layout.


```bash
mix new nova --sup
cd nova
mkdir -p lib/nova/{transport,template,websocket}
mkdir -p test/nova bench
```

### Step 2: `mix.exs`

**Objective**: Declare the Mix project configuration and third-party dependencies.


```elixir
defp deps do
  [
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/nova/conn.ex`

**Objective**: Model the connection struct that flows through the request pipeline.


```elixir
defmodule Nova.Conn do
  @moduledoc "Request/response struct — the 'conn' flowing through the pipeline."

  defstruct [
    :method, :path, :query, :version, :headers, :body,
    :path_segments, :params,
    status: 200,
    resp_headers: [],
    resp_body: "",
    halted: false
  ]

  @type t :: %__MODULE__{}

  def send_resp(%__MODULE__{} = conn, status, body) do
    %{conn | status: status, resp_body: body}
  end
end
```

### Step 4: `lib/nova/transport/http_parser.ex`

**Objective**: Parse raw HTTP bytes into structured requests with headers and body.


```elixir
defmodule Nova.Transport.HttpParser do
  @moduledoc """
  Parses HTTP/1.1 requests from raw binary data.

  Wire format:
    METHOD SP request-target SP HTTP/version CRLF
    *(header-field CRLF)
    CRLF
    message-body

  The parser is a state machine: :request_line -> :headers -> :body -> :done.
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
        case String.split(line, " ", parts: 3) do
          [method, target, version] ->
            {path, query} = split_target(target)
            {:ok, {method, path, query, version}, rest}

          _ ->
            {:error, :invalid_request_line}
        end

      [_incomplete] ->
        :incomplete
    end
  end

  defp split_target(target) do
    case String.split(target, "?", parts: 2) do
      [path, query] -> {path, query}
      [path] -> {path, nil}
    end
  end

  defp parse_headers(data) do
    case :binary.split(data, "\r\n\r\n") do
      [headers_block, rest] ->
        headers =
          headers_block
          |> String.split(@crlf)
          |> Enum.reject(&(&1 == ""))
          |> Enum.map(fn line ->
            case String.split(line, ": ", parts: 2) do
              [name, value] -> {String.downcase(name), value}
              [name] -> {String.downcase(name), ""}
            end
          end)

        {:ok, headers, rest}

      [_incomplete] ->
        :incomplete
    end
  end

  defp parse_body(data, headers) do
    content_length = get_header(headers, "content-length")
    transfer_encoding = get_header(headers, "transfer-encoding")

    cond do
      transfer_encoding == "chunked" ->
        parse_chunked_body(data, <<>>)

      content_length != nil ->
        len = String.to_integer(content_length)

        if byte_size(data) >= len do
          <<body::binary-size(len), rest::binary>> = data
          {:ok, body, rest}
        else
          :incomplete
        end

      true ->
        {:ok, "", data}
    end
  end

  defp parse_chunked_body(data, acc) do
    case :binary.split(data, @crlf) do
      [size_hex, rest] ->
        chunk_size = String.to_integer(String.trim(size_hex), 16)

        if chunk_size == 0 do
          case :binary.split(rest, @crlf) do
            [_, final_rest] -> {:ok, acc, final_rest}
            _ -> {:ok, acc, rest}
          end
        else
          if byte_size(rest) >= chunk_size + 2 do
            <<chunk::binary-size(chunk_size), @crlf, remaining::binary>> = rest
            parse_chunked_body(remaining, acc <> chunk)
          else
            :incomplete
          end
        end

      [_] ->
        :incomplete
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

### Step 5: `lib/nova/router.ex`

**Objective**: Match incoming requests against compiled routes and dispatch to handlers.


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

  At compile time, __before_compile__ generates match/3 function clauses
  with pattern matching for O(1) dispatch.
  """

  defmacro __using__(_) do
    quote do
      import Nova.Router
      Module.register_attribute(__MODULE__, :nova_routes, accumulate: true)
      Module.put_attribute(__MODULE__, :nova_scope_prefix, "")
      @before_compile Nova.Router
    end
  end

  defmacro __before_compile__(env) do
    routes = Module.get_attribute(env.module, :nova_routes) |> Enum.reverse()

    clauses =
      Enum.map(routes, fn {method, path, controller, action} ->
        segments = String.split(path, "/", trim: true)

        {pattern_ast, param_bindings} =
          Enum.map_reduce(segments, [], fn seg, acc ->
            if String.starts_with?(seg, ":") do
              param_name = String.trim_leading(seg, ":") |> String.to_atom()
              var = Macro.var(param_name, nil)
              {var, [{Atom.to_string(param_name), var} | acc]}
            else
              {seg, acc}
            end
          end)

        params_map =
          case param_bindings do
            [] ->
              quote do: %{}

            bindings ->
              pairs = Enum.map(bindings, fn {k, v} -> {k, v} end)
              {:%{}, [], pairs}
          end

        quote do
          def match(unquote(method), unquote(pattern_ast), conn) do
            unquote(controller).unquote(action)(conn, unquote(params_map))
          end
        end
      end)

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
      prefix = Module.get_attribute(__MODULE__, :nova_scope_prefix) || ""
      full_path = if prefix == "", do: unquote(path), else: prefix <> unquote(path)
      @nova_routes {"GET", full_path, unquote(controller), unquote(action)}
    end
  end

  defmacro post(path, controller, action) do
    quote do
      prefix = Module.get_attribute(__MODULE__, :nova_scope_prefix) || ""
      full_path = if prefix == "", do: unquote(path), else: prefix <> unquote(path)
      @nova_routes {"POST", full_path, unquote(controller), unquote(action)}
    end
  end

  defmacro put(path, controller, action) do
    quote do
      prefix = Module.get_attribute(__MODULE__, :nova_scope_prefix) || ""
      full_path = if prefix == "", do: unquote(path), else: prefix <> unquote(path)
      @nova_routes {"PUT", full_path, unquote(controller), unquote(action)}
    end
  end

  defmacro delete(path, controller, action) do
    quote do
      prefix = Module.get_attribute(__MODULE__, :nova_scope_prefix) || ""
      full_path = if prefix == "", do: unquote(path), else: prefix <> unquote(path)
      @nova_routes {"DELETE", full_path, unquote(controller), unquote(action)}
    end
  end

  defmacro patch(path, controller, action) do
    quote do
      prefix = Module.get_attribute(__MODULE__, :nova_scope_prefix) || ""
      full_path = if prefix == "", do: unquote(path), else: prefix <> unquote(path)
      @nova_routes {"PATCH", full_path, unquote(controller), unquote(action)}
    end
  end

  defmacro scope(prefix, do: block) do
    quote do
      old_prefix = Module.get_attribute(__MODULE__, :nova_scope_prefix)
      Module.put_attribute(__MODULE__, :nova_scope_prefix, unquote(prefix))
      unquote(block)
      Module.put_attribute(__MODULE__, :nova_scope_prefix, old_prefix)
    end
  end
end
```

### Step 6: `lib/nova/websocket/handshake.ex`

**Objective**: Perform the protocol handshake that upgrades the connection.


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
    headers = conn.headers || []
    upgrade = find_header(headers, "upgrade")
    connection = find_header(headers, "connection")

    upgrade != nil and String.downcase(upgrade) == "websocket" and
      connection != nil and String.downcase(connection) =~ "upgrade"
  end

  @doc """
  Builds the 101 Switching Protocols response headers.
  Returns the raw HTTP response bytes to write to the socket.
  """
  @spec build_response(String.t()) :: binary()
  def build_response(client_key) do
    accept = compute_accept(client_key)

    "HTTP/1.1 101 Switching Protocols\r\n" <>
      "Upgrade: websocket\r\n" <>
      "Connection: Upgrade\r\n" <>
      "Sec-WebSocket-Accept: #{accept}\r\n\r\n"
  end

  @spec compute_accept(String.t()) :: String.t()
  def compute_accept(client_key) do
    :crypto.hash(:sha, client_key <> @magic_guid)
    |> Base.encode64()
  end

  defp find_header(headers, name) do
    case List.keyfind(headers, name, 0) do
      {_, value} -> value
      nil -> nil
    end
  end
end
```

### Step 7: `lib/nova/session.ex`

**Objective**: Manage connection sessions with lifecycle, state, and cleanup.


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
    :crypto.mac(:hmac, :sha256, secret_key, data)
    |> Base.url_encode64(padding: false)
  end

  defp secure_compare(a, b) when byte_size(a) != byte_size(b), do: false

  defp secure_compare(a, b) do
    a_bytes = :binary.bin_to_list(a)
    b_bytes = :binary.bin_to_list(b)

    result =
      Enum.zip(a_bytes, b_bytes)
      |> Enum.reduce(0, fn {x, y}, acc -> Bitwise.bor(acc, Bitwise.bxor(x, y)) end)

    result == 0
  end
end
```

### Step 8: Given tests — must pass without modification

**Objective**: Validate behavior against the frozen test suite that must pass unmodified.


```elixir
# test/nova/http_parser_test.exs
defmodule Nova.HttpParserTest do
  use ExUnit.Case, async: true

  alias Nova.Transport.HttpParser


  describe "HttpParser" do

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
end
```

```elixir
# test/nova/session_test.exs
defmodule Nova.SessionTest do
  use ExUnit.Case, async: true

  alias Nova.Session

  @secret "test_secret_key_minimum_32_bytes_long"


  describe "Session" do

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
end
```

```elixir
# test/nova/websocket_test.exs
defmodule Nova.WebSocketTest do
  use ExUnit.Case, async: true

  alias Nova.WebSocket.Handshake


  describe "WebSocket" do

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
end
```

### Step 9: Run the tests

**Objective**: Execute the provided test suite to verify the implementation passes.


```bash
mix test test/nova/ --trace
```

### Step 10: Router benchmark

**Objective**: Measure throughput and latency characteristics under representative load.


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

Expected: dispatch with 100 routes should be under 1us. If you see > 10us, verify your `match/3` clauses are true pattern-match clauses — not a `cond` or `Enum.find` at runtime.

---

### Why this works

The design separates concerns along their real axes: what must be correct (the Phoenix-like web framework invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.

## Benchmark

```elixir
# Minimal timing harness — replace with Benchee for production measurement.
{time_us, _result} = :timer.tc(fn ->
  # exercise the hot path N times
  for _ <- 1..10_000, do: :ok
end)

IO.puts("average: #{time_us / 10_000} µs per op")
def main do
  IO.puts("[Nova.Conn.send_resp] demo")
  :ok
end

```

Target: <1µs dispatch for a 100-route application.

## Key Concepts: Event Sourcing and Immutable Logs

Event sourcing inverts the traditional database model: instead of storing current state, store every state-changing event in an immutable log. The current state is derived by replaying events from the start.

This shift has profound implications:
- **Audit trail is free**: Every change is a named event with timestamp and actor.
- **Temporal queries are simple**: Replay events up to a past date to see historical state.
- **Concurrency is safe**: Events are immutable and append-only, eliminating race conditions on state mutations.
- **Testability is easier**: Given a sequence of events, the state is deterministic; no mocks needed.

The BEAM is naturally suited for this pattern. Each aggregate (e.g., Account) is a GenServer that receives commands, validates them against current state, publishes an event if valid, then applies the event to update local state. The OTP supervision tree ensures persistence across restarts; the event log (in a database) survives the entire system.

The downside: evolving schemas is hard. If you rename a field or split an event type, old events still use the old structure. Solutions include versioning (introduce `withdrew_v2` alongside `withdrew_v1`) or upcasting (projection functions that translate old events to new). Frameworks like Commanded automate this.

Another challenge: reads require replaying events, which is slow for 10-year-old aggregates with millions of events. Solution: snapshots. Periodically serialize current state; replay only events after the snapshot. This trades disk space for query speed, a worthwhile tradeoff for most systems.

**Production insight**: Event sourcing is powerful for audit-heavy systems (banking, compliance), but unnecessary overhead for simple CRUD apps. Choose event sourcing when the audit trail or temporal queries justify the implementation complexity.

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
Calling `EEx.eval_file/2` on every request re-reads and re-compiles the template file. This adds 10-100ms per request and burns I/O. Compile with `EEx.compile_file/1` once at application start and store the compiled AST.

**5. Path segment matching with URL encoding**
A client may request `/users/alice%20smith`. Your path splitter must URL-decode each segment before matching. Failure to do so means `%20` never matches a `:name` capture for `"alice smith"`.

---

## Reflection

Your framework compiles 10k routes into function heads. Do you hit a beam file size limit, compile-time limit, or dispatch-time limit first? Where would you split routers?

## Resources

- [RFC 7230 -- HTTP/1.1 Message Syntax](https://www.rfc-editor.org/rfc/rfc7230) — sections 3-5 cover the wire format your parser must implement
- [RFC 6455 -- The WebSocket Protocol](https://www.rfc-editor.org/rfc/rfc6455) — section 1.3 (opening handshake) and section 5 (framing) are the two pieces you implement
- [Phoenix Framework source](https://github.com/phoenixframework/phoenix) — study `Phoenix.Router` for the macro pattern; `Phoenix.Socket` for channel multiplexing
- [Plug specification](https://hexdocs.pm/plug/readme.html) — your `Nova.Plug` behaviour should be compatible so existing Plug middlewares can be adapted
- ["Programming Phoenix 1.4"](https://pragprog.com/titles/phoenix14/programming-phoenix-1-4/) — McCord, Tate, Valim — chapters on Router internals and Channel architecture
