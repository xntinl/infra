# Tuples and Pattern Matching: Building an HTTP Response Handler

**Project**: `http_client` — a response processor that pattern matches on HTTP result tuples

---

## Why tuples and pattern matching replace exceptions

In Java or Python, HTTP client errors typically throw exceptions. The caller wraps
everything in `try/catch` and hopes they caught the right exception type. In Elixir,
the convention is different: functions return tagged tuples like `{:ok, result}` or
`{:error, reason}`, and callers use pattern matching to handle each case explicitly.

This is not just style — it is a design decision with real consequences:

1. **Exhaustiveness**: `case` on a tuple forces you to handle all variants. An uncaught
   exception silently propagates until something crashes.
2. **Composability**: tuples can be piped through `with` chains. Exceptions cannot.
3. **Testability**: returning `{:error, :timeout}` is easy to assert. Catching
   `%HTTPoison.Error{reason: :timeout}` requires exception-specific test infrastructure.

Tuples in Elixir are fixed-size, contiguous in memory, and O(1) to access by index.
They are perfect for small, fixed-structure return values — but wrong for collections
(use lists) or named fields (use maps/structs).

---

## The business problem

Your service calls multiple external APIs. Each response comes as a tuple
`{status_code, headers, body}`. You need a response processor that:

1. Pattern matches on status code ranges (2xx, 4xx, 5xx)
2. Extracts specific headers using tuple/list pattern matching
3. Parses JSON bodies conditionally
4. Composes multiple API calls using `with` for early exit on failure

---

## Project structure

```
http_client/
├── lib/
│   └── http_client/
│       ├── api.ex
│       └── response.ex
├── script/
│   └── main.exs
├── test/
│   └── http_client/
│       ├── api_test.exs
│       └── response_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — `{:ok, value}` / `{:error, reason}` tagged tuples**
- Pros: Pattern-matchable at call sites, forces the caller to acknowledge errors, zero runtime cost
- Cons: Slightly more typing than `try/rescue`, error must be bubbled manually

**Option B — raising exceptions for non-exceptional failures** (chosen)
- Pros: Familiar to OOP developers, stack trace included
- Cons: Control-flow via exceptions is expensive on BEAM, errors become invisible in APIs, `try/rescue` everywhere

→ Chose **A** because HTTP responses are the textbook example of expected-but-not-successful outcomes — they belong in the return value, not the exception channel.

## Implementation

### `mix.exs`
```elixir
defmodule HttpClient.MixProject do
  use Mix.Project

  def project do
    [
      app: :http_client,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```
### `lib/http_client.ex`

```elixir
defmodule HttpClient do
  @moduledoc """
  Tuples and Pattern Matching: Building an HTTP Response Handler.

  In Java or Python, HTTP client errors typically throw exceptions. The caller wraps.
  """
end
```
### `lib/http_client/response.ex`

```elixir
defmodule HttpClient.Response do
  @moduledoc """
  Processes HTTP response tuples into domain results.

  Follows the Elixir convention where every function returns
  {:ok, value} or {:error, reason} — never raises for expected failures.
  """

  @type headers :: [{String.t(), String.t()}]
  @type raw_response :: {pos_integer(), headers(), String.t()}

  @doc """
  Processes an HTTP response tuple into a domain result.

  Pattern matches on the status code to determine success or failure,
  then parses the body accordingly.

  ## Examples

      iex> HttpClient.Response.process_value({200, [{"content-type", "application/json"}], ~s({"user": "alice"})})
      {:ok, %{"user" => "alice"}}

      iex> HttpClient.Response.process_value({404, [], "Not Found"})
      {:error, {:client_error, 404, "Not Found"}}

      iex> HttpClient.Response.process_value({500, [], "Internal Server Error"})
      {:error, {:server_error, 500, "Internal Server Error"}}

  """
  @spec process_value(raw_response()) :: {:ok, term()} | {:error, term()}
  def process_value({status, headers, body}) when status >= 200 and status < 300 do
    if json_content?(headers) do
      parse_json_body(body)
    else
      {:ok, body}
    end
  end

  def process_value({status, _headers, body}) when status >= 400 and status < 500 do
    {:error, {:client_error, status, body}}
  end

  def process_value({status, _headers, body}) when status >= 500 do
    {:error, {:server_error, status, body}}
  end

  def process_value({status, _headers, body}) when status >= 300 and status < 400 do
    {:error, {:redirect, status, body}}
  end

  @doc """
  Extracts a specific header value from a headers list.

  Headers are stored as a list of {key, value} tuples. This function
  searches case-insensitively, matching real HTTP header behavior.

  ## Examples

      iex> headers = [{"Content-Type", "application/json"}, {"X-Request-Id", "abc123"}]
      iex> HttpClient.Response.get_header(headers, "content-type")
      {:ok, "application/json"}

      iex> HttpClient.Response.get_header([], "content-type")
      :error

  """
  @spec get_header(headers(), String.t()) :: {:ok, String.t()} | :error
  def get_header(headers, name) when is_list(headers) and is_binary(name) do
    downcased = String.downcase(name)

    case Enum.find(headers, fn {key, _val} -> String.downcase(key) == downcased end) do
      {_key, value} -> {:ok, value}
      nil -> :error
    end
  end

  @doc """
  Checks if a response indicates a retryable error.

  Status 429 (rate limited) and 5xx errors are retryable.
  Client errors (4xx except 429) are not.

  ## Examples

      iex> HttpClient.Response.retryable?({429, [], "Rate Limited"})
      true

      iex> HttpClient.Response.retryable?({503, [], "Service Unavailable"})
      true

      iex> HttpClient.Response.retryable?({404, [], "Not Found"})
      false

      iex> HttpClient.Response.retryable?({200, [], "OK"})
      false

  """
  @spec retryable?(raw_response()) :: boolean()
  def retryable?({429, _headers, _body}), do: true
  def retryable?({status, _headers, _body}) when status >= 500, do: true
  def retryable?({_status, _headers, _body}), do: false

  @spec json_content?(headers()) :: boolean()
  defp json_content?(headers) do
    case get_header(headers, "content-type") do
      {:ok, content_type} -> String.contains?(content_type, "json")
      :error -> false
    end
  end

  @spec parse_json_body(String.t()) :: {:ok, term()} | {:error, :invalid_json}
  defp parse_json_body(body) do
    case Jason.decode(body) do
      {:ok, data} -> {:ok, data}
      {:error, _} -> {:error, :invalid_json}
    end
  end
end
```
### `lib/http_client/api.ex`

```elixir
defmodule HttpClient.Api do
  @moduledoc """
  Composes multiple API calls using `with` for early exit on failure.

  Demonstrates how tagged tuples compose through `with` chains —
  the functional equivalent of try/catch with multiple operations.
  """

  alias HttpClient.Response

  @doc """
  Fetches a user profile by first getting the user ID from an auth token,
  then fetching the profile with that ID.

  Uses `with` to chain two API calls. If the first fails, the second
  never executes — `with` short-circuits on the first non-matching clause.

  The `http_client` parameter is a function that simulates HTTP calls,
  making this testable without real network access.

  ## Examples

      iex> client = fn
      ...>   :get, "/auth/me" -> {200, [{"content-type", "application/json"}], ~s({"id": "u123"})}
      ...>   :get, "/users/u123" -> {200, [{"content-type", "application/json"}], ~s({"name": "Alice", "email": "alice@example.com"})}
      ...> end
      iex> {:ok, profile} = HttpClient.Api.fetch_user_profile(client)
      iex> profile["name"]
      "Alice"

  """
  @spec fetch_user_profile((atom(), String.t() -> Response.raw_response())) ::
          {:ok, map()} | {:error, term()}
  def fetch_user_profile(http_client) do
    with {:ok, %{"id" => user_id}} <-
           http_client.(:get, "/auth/me") |> Response.process(),
         {:ok, profile} <-
           http_client.(:get, "/users/#{user_id}") |> Response.process() do
      {:ok, profile}
    end
  end

  @doc """
  Fetches data from multiple endpoints and merges results.

  Demonstrates pattern matching on tuples within Enum operations.
  Collects all successful results and all errors separately.

  ## Examples

      iex> client = fn
      ...>   :get, "/status" -> {200, [{"content-type", "application/json"}], ~s({"healthy": true})}
      ...>   :get, "/version" -> {200, [{"content-type", "application/json"}], ~s({"version": "1.0"})}
      ...>   :get, "/broken" -> {500, [], "down"}
      ...> end
      iex> {successes, errors} = HttpClient.Api.fetch_all(client, ["/status", "/version", "/broken"])
      iex> length(successes)
      2
      iex> length(errors)
      1

  """
  @spec fetch_all(
          (atom(), String.t() -> Response.raw_response()),
          [String.t()]
        ) :: {[{String.t(), term()}], [{String.t(), term()}]}
  def fetch_all(http_client, paths) do
    results =
      Enum.map(paths, fn path ->
        {path, http_client.(:get, path) |> Response.process()}
      end)

    successes =
      for {path, {:ok, data}} <- results, do: {path, data}

    errors =
      for {path, {:error, reason}} <- results, do: {path, reason}

    {successes, errors}
  end
end
```
### `test/http_client_test.exs`
```elixir
defmodule HttpClient.ResponseTest do
  use ExUnit.Case, async: true

  alias HttpClient.Response

  doctest HttpClient.Response

  describe "process/1" do
    test "parses 200 with JSON body" do
      response = {200, [{"content-type", "application/json"}], ~s({"key": "value"})}
      assert {:ok, %{"key" => "value"}} = Response.process(response)
    end

    test "returns raw body for non-JSON 200" do
      response = {200, [{"content-type", "text/plain"}], "hello"}
      assert {:ok, "hello"} = Response.process(response)
    end

    test "returns client error for 4xx" do
      response = {404, [], "Not Found"}
      assert {:error, {:client_error, 404, "Not Found"}} = Response.process(response)
    end

    test "returns server error for 5xx" do
      response = {503, [], "Service Unavailable"}
      assert {:error, {:server_error, 503, "Service Unavailable"}} = Response.process(response)
    end

    test "returns redirect for 3xx" do
      response = {301, [], "Moved"}
      assert {:error, {:redirect, 301, "Moved"}} = Response.process(response)
    end

    test "returns error for invalid JSON in 200" do
      response = {200, [{"content-type", "application/json"}], "not json"}
      assert {:error, :invalid_json} = Response.process(response)
    end
  end

  describe "get_header/2" do
    test "finds header case-insensitively" do
      headers = [{"Content-Type", "application/json"}, {"X-Request-Id", "abc"}]
      assert {:ok, "application/json"} = Response.get_header(headers, "content-type")
      assert {:ok, "abc"} = Response.get_header(headers, "x-request-id")
    end

    test "returns :error for missing header" do
      assert :error = Response.get_header([], "content-type")
    end
  end

  describe "retryable?/1" do
    test "429 is retryable" do
      assert Response.retryable?({429, [], "Rate Limited"})
    end

    test "5xx is retryable" do
      assert Response.retryable?({500, [], "Error"})
      assert Response.retryable?({503, [], "Unavailable"})
    end

    test "4xx (except 429) is not retryable" do
      refute Response.retryable?({400, [], "Bad Request"})
      refute Response.retryable?({404, [], "Not Found"})
    end

    test "2xx is not retryable" do
      refute Response.retryable?({200, [], "OK"})
    end
  end
end
```
```elixir
defmodule HttpClient.ApiTest do
  use ExUnit.Case, async: true

  alias HttpClient.Api

  doctest HttpClient.Api

  describe "fetch_user_profile/1" do
    test "chains two successful calls" do
      client = fn
        :get, "/auth/me" ->
          {200, [{"content-type", "application/json"}], ~s({"id": "u42"})}

        :get, "/users/u42" ->
          {200, [{"content-type", "application/json"}], ~s({"name": "Bob"})}
      end

      assert {:ok, %{"name" => "Bob"}} = Api.fetch_user_profile(client)
    end

    test "short-circuits when auth fails" do
      client = fn
        :get, "/auth/me" -> {401, [], "Unauthorized"}
        :get, "/users/" <> _ -> raise ArgumentError, "should not be called"
      end

      assert {:error, {:client_error, 401, "Unauthorized"}} =
               Api.fetch_user_profile(client)
    end

    test "fails when profile fetch fails" do
      client = fn
        :get, "/auth/me" ->
          {200, [{"content-type", "application/json"}], ~s({"id": "u42"})}

        :get, "/users/u42" ->
          {500, [], "Database down"}
      end

      assert {:error, {:server_error, 500, "Database down"}} =
               Api.fetch_user_profile(client)
    end
  end

  describe "fetch_all/2" do
    test "separates successes from errors" do
      client = fn
        :get, "/ok" -> {200, [{"content-type", "application/json"}], ~s({"status": "up"})}
        :get, "/fail" -> {500, [], "down"}
      end

      {successes, errors} = Api.fetch_all(client, ["/ok", "/fail"])
      assert length(successes) == 1
      assert length(errors) == 1
    end
  end
end
```
### Run the tests

```bash
mix test --trace
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== HttpClient: demo ===\n")

    result_1 = HttpClient.Response.process({200, [{"content-type", "application/json"}], ~s({"user": "alice"})})
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = HttpClient.Response.process({404, [], "Not Found"})
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = HttpClient.Response.process({500, [], "Internal Server Error"})
    IO.puts("Demo 3: #{inspect(result_3)}")

    IO.puts("\n=== Done ===")
  end
end

Main.main()
```
Run with: `elixir script/main.exs`

---

### Why this works

The approach chosen above keeps the core logic **pure, pattern-matchable, and testable**. Each step is a small, named transformation with an explicit return shape, so adding a new case means adding a new clause — not editing a branching block. Failures are data (`{:error, reason}`), not control-flow, which keeps the hot path linear and the error path explicit.

---

Create the module files and test them in `iex`:

```elixir
defmodule Response do
  def process_value({status, headers, body}) when status in 200..299 do
    case parse_json(body) do
      {:ok, data} -> {:ok, data}
      :error -> {:error, :invalid_json}
    end
  end

  def process_value({status, _headers, body}) when status in 300..399 do
    {:error, {:redirect, status, body}}
  end

  def process_value({status, _headers, body}) when status in 400..499 do
    {:error, {:client_error, status, body}}
  end

  def process_value({status, _headers, body}) when status >= 500 do
    {:error, {:server_error, status, body}}
  end

  def get_header(headers, name) do
    found = Enum.find(headers, fn {key, _value} ->
      String.downcase(key) == String.downcase(name)
    end)
    case found do
      {_key, value} -> {:ok, value}
      nil -> :error
    end
  end

  def retryable?({429, _, _}), do: true
  def retryable?({status, _, _}) when status >= 500, do: true
  def retryable?(_), do: false

  defp parse_json(json_str) do
    try do
      case Jason.decode(json_str) do
        {:ok, data} -> {:ok, data}
        {:error, _} -> :error
      end
    rescue
      _ -> :error
    end
  end
end

# Test responses
ok_response = {200, [{"content-type", "application/json"}], ~s({"id": 42})}
{:ok, data} = Response.process_value(ok_response)
IO.inspect(data)  # %{"id" => 42}

error_response = {500, [], "Server Error"}
{:error, {:server_error, 500, _}} = Response.process_value(error_response)
IO.puts("Error response matched")

IO.inspect(Response.retryable?({429, [], "Rate Limited"}))  # true
IO.inspect(Response.retryable?({200, [], "OK"}))  # false

headers = [{"Content-Type", "application/json"}, {"X-Request-Id", "abc"}]
{:ok, ct} = Response.get_header(headers, "content-type")
IO.inspect(ct)  # "application/json"
```
## When to use tuples vs other data structures

| Data structure | Access | Size | Use when |
|----------------|--------|------|----------|
| Tuple `{a, b}` | O(1) by index | Fixed, small (2-4 elements) | Return values, coordinates, tagged results |
| List `[a, b]` | O(n) by index, O(1) prepend | Variable | Collections, sequences |
| Map `%{k: v}` | O(log n) by key | Variable | Named fields, lookups |
| Struct `%S{}` | O(log n) by key | Fixed keys | Domain entities |

Tuples with more than 4 elements are a code smell. If you find yourself writing
`{status, headers, body, timestamp, request_id}`, use a map or struct instead.

---

## Benchmark

```elixir
{time_us, _result} =
  :timer.tc(fn ->
    for _ <- 1..1_000 do
      # representative call of pattern match on `{:ok, body}` over 1M iterations
      :ok
    end
  end)

IO.puts("Avg: #{time_us / 1_000} µs/call")
```
Target: **< 10ms total; BEAM pattern matching on small tuples is essentially free**.

## Common production mistakes

**1. Using `elem/2` instead of pattern matching**
```elixir
# Bad — loses the compiler's ability to check tuple shape
status = elem(response, 0)

# Good — compiler sees the expected shape
{status, _headers, _body} = response
```
**2. Matching the wrong tuple size**
```elixir
# This crashes if the function returns {:error, reason, context}
{:error, reason} = some_function()  # MatchError if 3-element tuple
```
Always check the function's typespec or documentation for all possible tuple shapes.

**3. Ignoring the error case in `with`**
```elixir
with {:ok, a} <- step1(),
     {:ok, b} <- step2(a) do
  {:ok, b}
end
# If step1 returns {:error, reason}, `with` returns it unchanged.
# Make sure the caller handles this.
```
**4. Raising on expected failures**
If a file might not exist, `File.read/1` returns `{:error, :enoent}`. Use pattern
matching. Do not use `File.read!/1` (which raises) unless you genuinely want to crash.

---

## Reflection

In a request that fails 50% of the time, which would use less CPU: `{:error, reason}` tuples or raising exceptions? Where does the cost come from?

How does the choice of tagged tuples change your HTTP client API versus an OOP language where methods raise on non-2xx responses?

## Resources

- [Tuples — Elixir Getting Started](https://elixir-lang.org/getting-started/basic-types.html#tuples)
- [Pattern matching — Elixir Getting Started](https://elixir-lang.org/getting-started/pattern-matching.html)
- [with — Kernel.SpecialForms](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#with/1)
- [Tuple — HexDocs](https://hexdocs.pm/elixir/Tuple.html)

---

## Why Tuples and Pattern Matching matters

Mastering **Tuples and Pattern Matching** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts
### 1. Pattern Matching is Unification, Not Equality

Pattern matching binds variables to values by structure. The pattern `{x, y}` unifies with `{1, 2}`, binding `x` to `1` and `y` to `2`. If you want to match a literal value, you must write it: `{1, y} = {1, 2}` binds `y`; `{1, y} = {2, 2}` raises `MatchError`.

This is why you can build multi-clause functions: each clause patterns on different structures, and Elixir tries them in order until one matches.

### 2. Tuples Are Fixed-Size, Ordered Structures

Tuples are stackable—fixed at compile time and stored contiguously in memory. Lists are linked and grow at the head. Use tuples for return values with known arity (`{:ok, result}`), lightweight records, and pattern matching on structure. For dynamic collections, use lists or maps.

### 3. Compound Patterns Nest

You can match nested structures arbitrarily deep: `{:ok, {id, status}} = {:ok, {123, :pending}}`. This is powerful for extracting deeply nested data, but it can become brittle if the API changes shape. Consider building wrapper modules with helper functions.

---
