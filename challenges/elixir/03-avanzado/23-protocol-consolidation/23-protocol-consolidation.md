# Protocol Consolidation and Dispatch Performance

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The gateway serializes request and response data
to multiple formats (JSON body, log lines, telemetry metadata) depending on the
context. Different parts of the codebase produce different struct types: `Conn`,
`Route`, `RateLimitResult`, `AuthToken`, `ErrorResponse`. All need to be serializable
to a log-friendly string and to a telemetry-friendly map.

The team uses two protocols:
- `ApiGateway.Serializable` — converts a value to a map for JSON encoding
- `ApiGateway.Loggable` — converts a value to a flat string for structured logs

As the number of protocol implementations grows, the protocol dispatch path shows
up in profiling. This exercise covers how Elixir dispatches protocols, what
consolidation does, and how to design protocols for zero-overhead dispatch in prod.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── protocols/
│       │   ├── serializable.ex
│       │   └── loggable.ex
│       ├── middleware/
│       │   ├── pipeline.ex
│       │   ├── instrumentation.ex
│       │   └── dsl.ex
│       └── ...
├── test/
│   └── api_gateway/
│       └── protocols/
│           └── protocols_test.exs
├── bench/
│   └── protocol_bench.exs
└── mix.exs
```

---

## The business problem

Two requirements:

1. **Uniform serialization**: every struct in the gateway must implement
   `ApiGateway.Serializable` so that JSON encoding, telemetry metadata maps, and
   HTTP response construction all go through one dispatch path. New struct types
   added by contributors get a compile error if they forget to implement the
   protocol — not a runtime `Protocol.UndefinedError` in production.

2. **Log-friendly representation**: every event emitted to the structured log
   (`:logger`, `:telemetry`) must produce a flat string via `ApiGateway.Loggable`.
   Structs that are "obviously loggable" from their `Serializable` implementation
   should use `@derive` instead of writing a redundant implementation.

---

## How Elixir protocol dispatch works

### Development: dynamic dispatch, O(n)

In `Mix.env() == :dev`, every `Protocol.dispatch/1` call walks a list of known
implementations at runtime to find the right module. Because hot code reload can
add a new implementation at any time, the VM cannot cache the result.

```
Serializable.to_map(value)
  → typeof(value) = ApiGateway.Conn
  → scan [:Conn, :Route, :RateLimitResult, ...] for matching module
  → O(n) where n = number of implementations
```

### Production: consolidated dispatch, O(1)

`mix compile` (or `Protocol.consolidate/2`) generates a single optimized module
where dispatch is compiled pattern matching — essentially a `case` over the type
tag. The result is a direct function call with no list scan.

```
Serializable.to_map(value)
  → typeof(value) → compiled case → direct call
  → O(1) regardless of how many implementations exist
```

**Benchmarks on typical gateway protocol with 10 implementations**:
- Development (no consolidation): ~800ns–1.2µs per dispatch
- Production (consolidated): ~60–120ns per dispatch

### `@derive` — automatic delegation

```elixir
defmodule ApiGateway.AuthToken do
  @derive [ApiGateway.Loggable]
  defstruct [:token, :user_id, :expires_at]
end
```

When `ApiGateway.Loggable` defines an `Any` implementation, `@derive` generates
a `Loggable` implementation for `AuthToken` that calls the `Any` fallback. This
avoids writing boilerplate implementations for structs whose loggable representation
is the same as the generic fallback.

### `Protocol.impl_for/1` — introspection

```elixir
Protocol.impl_for(%ApiGateway.Conn{})   # => ApiGateway.Serializable.ApiGateway.Conn
Protocol.impl_for(42)                   # => ApiGateway.Serializable.Integer (if implemented)
Protocol.impl_for(:unknown_atom)        # => nil (raises UndefinedError on dispatch)
```

Use `Protocol.impl_for!/1` in tests to assert that a struct implements a protocol —
it raises if no implementation exists, making missing implementations a test failure
rather than a production crash.

---

## Implementation

### Step 1: `lib/api_gateway/protocols/serializable.ex`

```elixir
defprotocol ApiGateway.Serializable do
  @moduledoc """
  Converts gateway structs to maps suitable for JSON encoding and telemetry metadata.

  Every struct that moves through the gateway pipeline must implement this protocol.
  The `to_map/1` return value must contain only JSON-safe values: strings, numbers,
  booleans, nil, lists, and maps. No atoms, no tuples, no pids.
  """

  @doc """
  Converts `value` to a map with string keys and JSON-safe values.
  """
  @spec to_map(t()) :: map()
  def to_map(value)
end

# ── Core gateway struct implementations ──────────────────────────────────────

defimpl ApiGateway.Serializable, for: Map do
  def to_map(map) do
    Map.new(map, fn {k, v} -> {to_string(k), v} end)
  end
end

# Implement for the gateway's own structs:

defmodule ApiGateway.Conn do
  @moduledoc "Represents an in-flight HTTP connection through the gateway."
  @derive [ApiGateway.Loggable]
  defstruct [:method, :path, :status, :remote_ip, :assigns]
end

defimpl ApiGateway.Serializable, for: ApiGateway.Conn do
  @doc """
  Serializes a Conn to a JSON-safe map. The remote_ip tuple is converted to
  a dotted-quad string because JSON cannot represent Erlang tuples.
  The assigns map is recursively serialized with string keys.
  """
  def to_map(%ApiGateway.Conn{} = conn) do
    ip_string =
      case conn.remote_ip do
        {a, b, c, d} -> "#{a}.#{b}.#{c}.#{d}"
        nil -> nil
        other -> to_string(other)
      end

    assigns_map =
      case conn.assigns do
        nil -> %{}
        m when is_map(m) -> Map.new(m, fn {k, v} -> {to_string(k), v} end)
      end

    %{
      "method" => conn.method,
      "path" => conn.path,
      "status" => conn.status,
      "remote_ip" => ip_string,
      "assigns" => assigns_map
    }
  end
end

defmodule ApiGateway.Route do
  @moduledoc "Represents a matched route definition."
  @derive [ApiGateway.Loggable]
  defstruct [:method, :pattern, :handler, :middleware]
end

defimpl ApiGateway.Serializable, for: ApiGateway.Route do
  @doc """
  Serializes a Route to a JSON-safe map. The handler module atom and
  middleware module list are converted to strings via inspect/1 because
  JSON cannot represent Elixir atoms.
  """
  def to_map(%ApiGateway.Route{} = route) do
    middleware_list =
      case route.middleware do
        nil -> []
        list -> Enum.map(list, &inspect/1)
      end

    %{
      "method" => route.method,
      "pattern" => route.pattern,
      "handler" => inspect(route.handler),
      "middleware" => middleware_list
    }
  end
end

defmodule ApiGateway.RateLimitResult do
  @moduledoc "Result of a rate limit check."
  defstruct [:allowed, :remaining, :reset_at, :limit]
end

defimpl ApiGateway.Serializable, for: ApiGateway.RateLimitResult do
  def to_map(%ApiGateway.RateLimitResult{} = r) do
    %{
      "allowed" => r.allowed,
      "remaining" => r.remaining,
      "reset_at" => r.reset_at,
      "limit" => r.limit
    }
  end
end

defmodule ApiGateway.ErrorResponse do
  @moduledoc "Structured error for HTTP error responses."
  @derive [ApiGateway.Loggable]
  defstruct [:status, :code, :message, :details]
end

defimpl ApiGateway.Serializable, for: ApiGateway.ErrorResponse do
  @doc """
  Serializes an ErrorResponse. The code atom is converted to a string
  because JSON encoders cannot encode bare atoms.
  """
  def to_map(%ApiGateway.ErrorResponse{} = err) do
    %{
      "status" => err.status,
      "code" => to_string(err.code),
      "message" => err.message,
      "details" => err.details
    }
  end
end
```

### Step 2: `lib/api_gateway/protocols/loggable.ex`

```elixir
defprotocol ApiGateway.Loggable do
  @moduledoc """
  Converts gateway values to a flat, human-readable string for structured logs.

  Log lines must be flat (no nested maps), short (under 200 chars), and include
  the struct type as a prefix: "conn GET /api/users status=200".
  """

  @fallback_to_any true

  @doc """
  Returns a compact, log-friendly string representation.
  """
  @spec to_log(t()) :: String.t()
  def to_log(value)
end

# Any fallback — used when @derive [ApiGateway.Loggable] is set on a struct.
# Produces a generic log line using the struct's module name as prefix and
# inspect/2 with a low limit to keep output compact.
defimpl ApiGateway.Loggable, for: Any do
  def to_log(%{__struct__: struct} = value) do
    prefix =
      struct
      |> Module.split()
      |> List.last()
      |> String.downcase()

    "#{prefix} #{inspect(value, limit: 5)}"
  end

  def to_log(value) do
    inspect(value, limit: 5)
  end
end

# Concrete implementations for gateway structs

defimpl ApiGateway.Loggable, for: ApiGateway.Conn do
  def to_log(%ApiGateway.Conn{method: method, path: path, status: status}) do
    status_str = if status, do: " status=#{status}", else: ""
    "conn #{method} #{path}#{status_str}"
  end
end

defimpl ApiGateway.Loggable, for: ApiGateway.RateLimitResult do
  def to_log(%ApiGateway.RateLimitResult{allowed: allowed, remaining: remaining}) do
    "rate_limit allowed=#{allowed} remaining=#{remaining}"
  end
end

# Route and ErrorResponse use the Any fallback via @derive
# (declared on the struct modules in serializable.ex with @derive [ApiGateway.Loggable])
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/protocols/protocols_test.exs
defmodule ApiGateway.ProtocolsTest do
  use ExUnit.Case, async: true

  alias ApiGateway.{Conn, Route, RateLimitResult, ErrorResponse}
  alias ApiGateway.{Serializable, Loggable}

  # ---------------------------------------------------------------------------
  # Serializable tests
  # ---------------------------------------------------------------------------

  describe "Serializable.to_map/1" do
    test "Conn produces map with string keys" do
      conn = %Conn{method: "GET", path: "/health", status: 200, remote_ip: {127, 0, 0, 1}, assigns: %{}}
      result = Serializable.to_map(conn)

      assert is_map(result)
      assert result["method"] == "GET"
      assert result["path"] == "/health"
      assert result["status"] == 200
      assert result["remote_ip"] == "127.0.0.1"
    end

    test "RateLimitResult serializes scalar fields" do
      result = %RateLimitResult{allowed: true, remaining: 42, reset_at: 1_700_000_000, limit: 100}
      map = Serializable.to_map(result)

      assert map["allowed"] == true
      assert map["remaining"] == 42
      assert map["limit"] == 100
    end

    test "ErrorResponse converts code atom to string" do
      err = %ErrorResponse{status: 429, code: :rate_limited, message: "Too many requests", details: nil}
      map = Serializable.to_map(err)

      assert map["status"] == 429
      assert map["code"] == "rate_limited"
      assert map["message"] == "Too many requests"
    end

    test "Route converts handler module to string" do
      route = %Route{method: "GET", pattern: "/users", handler: MyApp.UserHandler, middleware: []}
      map = Serializable.to_map(route)

      assert is_binary(map["handler"])
      assert String.contains?(map["handler"], "UserHandler")
    end

    test "all values in result are JSON-safe (no atoms, no tuples)" do
      conn = %Conn{method: "GET", path: "/test", status: 200, remote_ip: {10, 0, 0, 1}, assigns: %{}}
      map = Serializable.to_map(conn)

      Enum.each(map, fn {k, v} ->
        assert is_binary(k), "key #{inspect(k)} is not a string"
        assert is_binary(v) or is_number(v) or is_boolean(v) or is_nil(v) or is_map(v) or is_list(v),
               "value #{inspect(v)} for key #{k} is not JSON-safe"
      end)
    end
  end

  # ---------------------------------------------------------------------------
  # Loggable tests
  # ---------------------------------------------------------------------------

  describe "Loggable.to_log/1" do
    test "Conn produces compact log string" do
      conn = %Conn{method: "POST", path: "/api/users", status: 201, remote_ip: nil, assigns: %{}}
      log = Loggable.to_log(conn)

      assert is_binary(log)
      assert String.contains?(log, "POST")
      assert String.contains?(log, "/api/users")
      assert String.contains?(log, "201")
    end

    test "RateLimitResult log contains allowed and remaining" do
      result = %RateLimitResult{allowed: false, remaining: 0, reset_at: nil, limit: 10}
      log = Loggable.to_log(result)

      assert String.contains?(log, "allowed")
      assert String.contains?(log, "false")
    end

    test "ErrorResponse uses Any fallback via @derive" do
      err = %ErrorResponse{status: 500, code: :internal_error, message: "oops", details: nil}
      log = Loggable.to_log(err)

      assert is_binary(log)
      assert byte_size(log) < 500
    end

    test "log output is a flat string (no newlines)" do
      conn = %Conn{method: "GET", path: "/", status: 200, remote_ip: nil, assigns: %{}}
      log = Loggable.to_log(conn)

      refute String.contains?(log, "\n")
    end
  end

  # ---------------------------------------------------------------------------
  # Protocol introspection tests
  # ---------------------------------------------------------------------------

  describe "Protocol.impl_for/1" do
    test "all gateway structs implement Serializable" do
      structs = [
        %Conn{},
        %Route{},
        %RateLimitResult{},
        %ErrorResponse{}
      ]

      for s <- structs do
        assert Protocol.impl_for(s) != nil,
               "#{inspect(s.__struct__)} does not implement Serializable"
      end
    end

    test "Protocol.impl_for!/1 does not raise for gateway structs" do
      assert Serializable.impl_for!(%Conn{}) != nil
      assert Serializable.impl_for!(%RateLimitResult{}) != nil
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/api_gateway/protocols/protocols_test.exs --trace
```

### Step 5: Protocol dispatch benchmark

```elixir
# bench/protocol_bench.exs
alias ApiGateway.{Conn, Route, RateLimitResult, ErrorResponse, Serializable, Loggable}

conn = %Conn{method: "GET", path: "/api/users", status: 200, remote_ip: {127, 0, 0, 1}, assigns: %{}}
route = %Route{method: "GET", pattern: "/api/users", handler: MyHandler, middleware: []}
rate_result = %RateLimitResult{allowed: true, remaining: 99, reset_at: 1_700_000_000, limit: 100}
error = %ErrorResponse{status: 500, code: :internal, message: "err", details: nil}

values = [conn, route, rate_result, error]

Benchee.run(
  %{
    "Serializable.to_map (mixed types)" => fn ->
      for v <- values, do: Serializable.to_map(v)
    end,
    "Loggable.to_log (mixed types)" => fn ->
      for v <- values, do: Loggable.to_log(v)
    end,
    "Protocol.impl_for (dispatch only)" => fn ->
      for v <- values, do: Serializable.impl_for(v)
    end
  },
  warmup: 2,
  time: 5,
  formatters: [Benchee.Formatters.Console]
)
```

```bash
# Run in dev (unconsolidated):
MIX_ENV=dev mix run bench/protocol_bench.exs

# Run in prod (consolidated):
MIX_ENV=prod mix run bench/protocol_bench.exs
```

**Expected gap**: consolidated dispatch is 5–15x faster for protocols with 4+
implementations. The gap widens as the number of implementations grows because
dev dispatch scans a list while prod dispatch uses compiled pattern matching.

---

## Trade-off analysis

| Dispatch mode | Latency | Hot code reload | Implementation discovery | Use case |
|---------------|---------|-----------------|--------------------------|----------|
| Unconsolidated (dev) | O(n) ~800ns | Yes | Dynamic | Development only |
| Consolidated (prod) | O(1) ~80ns | No | Static (compile time) | Production |
| Direct function call | O(1) ~10ns | N/A | None — no polymorphism | Fixed types only |
| `@derive` with Any | O(1) prod | N/A | Via `@derive` declaration | Boilerplate reduction |

**When `@derive` is appropriate**: when a struct's implementation would be identical
or structurally similar to the `Any` fallback. When the implementation needs
struct-specific field access, write an explicit `defimpl`.

---

## Common production mistakes

**1. Forgetting `@fallback_to_any true` when using `@derive`**
If the protocol definition does not include `@fallback_to_any true`, then
`@derive [MyProtocol]` on a struct silently does nothing — no error, no implementation.
The first call raises `Protocol.UndefinedError` at runtime. Always set
`@fallback_to_any true` when the protocol is intended to support `@derive`.

**2. Returning atoms from `to_map/1`**
Protocols like `Serializable` are typically used before JSON encoding. JSON encoders
(`Jason`, `Poison`) cannot encode bare atoms (`:ok`, `:error`, `:admin`). Convert
all atoms to strings in the protocol implementation, not in the encoder.

**3. Using `Protocol.consolidate/2` manually in production**
`Protocol.consolidate/2` is a build tool — call it once at compile time. Calling it
at runtime (in `Application.start/2`) re-runs consolidation on every deploy start,
adding seconds to boot time and defeating the purpose of pre-consolidated builds.

**4. Not testing `Protocol.impl_for!/1` in CI**
New struct types added by contributors silently lack protocol implementations until
the code path that dispatches to them is hit in production. A simple test that calls
`Protocol.impl_for!/1` for every known struct catches this at CI time.

**5. Implementing the protocol for `Any` when you meant to implement it for a specific struct**
`defimpl MyProtocol, for: Any` applies to all types that don't have a specific
implementation. If `@fallback_to_any` is true, this silently handles types that
should have explicit implementations. Turn on `@derive` only for structs that
truly benefit from the generic fallback.

---

## Resources

- [Elixir Protocol documentation](https://hexdocs.pm/elixir/Protocol.html) — consolidation, `impl_for/1`, `@fallback_to_any`
- [Protocol.consolidate/2 — Elixir docs](https://hexdocs.pm/elixir/Protocol.html#consolidate/2) — manual consolidation API
- [Jason library source](https://github.com/michalmuskala/jason) — real-world protocol-based JSON encoding with `@derive`
- [Elixir guide: Protocols](https://elixir-lang.org/getting-started/protocols.html) — intro to polymorphism and `@derive`
- [Erlang match compilation — BEAM book](https://www.oreilly.com/library/view/the-erlang-runtime/9781800560818/) — how consolidated dispatch maps to pattern-match bytecode
