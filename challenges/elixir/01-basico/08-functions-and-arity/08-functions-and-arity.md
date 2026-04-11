# Functions and Arity: Building a Middleware Pipeline

**Project**: `pipeline` — a composable middleware system (like Plug) using function composition and multi-arity functions

---

## Why arity matters in Elixir

In Elixir, `process/1` and `process/2` are different functions — not overloads of
the same function. This is called arity (the number of arguments), and it is part
of the function's identity. When you see `Enum.map/2`, the `/2` is not documentation
decoration — it is how the runtime identifies the function.

This has practical consequences:

```elixir
# These are TWO DIFFERENT functions
def process(data), do: process(data, [])
def process(data, opts), do: # ...

# You reference them separately
&process/1  # captures the 1-arity version
&process/2  # captures the 2-arity version

# Default arguments generate multiple arities automatically
def process(data, opts \\ [])
# Generates process/1 and process/2 at compile time
```

Understanding arity is essential for: function capture (`&fun/n`), pattern matching
on function references, and designing APIs with default arguments.

---

## The business problem

Build a middleware pipeline system where:

1. Each middleware is a function that transforms a context map
2. Middlewares compose into a pipeline that runs sequentially
3. Any middleware can halt the pipeline (like Plug's `halt`)
4. The system uses multi-arity functions for configuration
5. Function capture (`&`) is used to reference middleware functions

---

## Implementation

### `lib/pipeline.ex`

```elixir
defmodule Pipeline do
  @moduledoc """
  A composable middleware pipeline inspired by Plug.

  Each middleware is a function that receives a context map and returns
  a (possibly modified) context map. The pipeline runs middlewares
  sequentially until all complete or one halts execution.
  """

  @type context :: %{
          required(:halted) => boolean(),
          required(:assigns) => map(),
          optional(atom()) => term()
        }

  @type middleware :: (context() -> context())

  @doc """
  Creates a new empty context.

  ## Examples

      iex> ctx = Pipeline.new_context()
      iex> ctx.halted
      false
      iex> ctx.assigns
      %{}

  """
  @spec new_context() :: context()
  def new_context do
    %{halted: false, assigns: %{}, status: nil, body: nil}
  end

  @doc """
  Creates a context with initial assigns.

  ## Examples

      iex> ctx = Pipeline.new_context(%{user_id: 42})
      iex> ctx.assigns.user_id
      42

  """
  @spec new_context(map()) :: context()
  def new_context(initial_assigns) when is_map(initial_assigns) do
    %{halted: false, assigns: initial_assigns, status: nil, body: nil}
  end

  @doc """
  Runs a list of middleware functions against a context.

  Stops execution when a middleware sets `halted: true`.

  ## Examples

      iex> add_header = fn ctx -> put_in(ctx, [:assigns, :powered_by], "Elixir") end
      iex> set_status = fn ctx -> %{ctx | status: 200} end
      iex> ctx = Pipeline.run([add_header, set_status], Pipeline.new_context())
      iex> ctx.assigns.powered_by
      "Elixir"
      iex> ctx.status
      200

  """
  @spec run([middleware()], context()) :: context()
  def run(middlewares, context) when is_list(middlewares) do
    Enum.reduce_while(middlewares, context, fn middleware, ctx ->
      if ctx.halted do
        {:halt, ctx}
      else
        {:cont, middleware.(ctx)}
      end
    end)
  end

  @doc """
  Assigns a key-value pair to the context's assigns map.

  This is a convenience function — the same as `put_in(ctx, [:assigns, key], value)`.

  ## Examples

      iex> ctx = Pipeline.new_context() |> Pipeline.assign(:role, :admin)
      iex> ctx.assigns.role
      :admin

  """
  @spec assign(context(), atom(), term()) :: context()
  def assign(context, key, value) when is_atom(key) do
    put_in(context, [:assigns, key], value)
  end

  @doc """
  Halts the pipeline. Subsequent middlewares will not execute.

  ## Examples

      iex> ctx = Pipeline.new_context() |> Pipeline.halt_pipeline()
      iex> ctx.halted
      true

  """
  @spec halt_pipeline(context()) :: context()
  def halt_pipeline(context) do
    %{context | halted: true}
  end
end
```

### `lib/pipeline/middlewares.ex`

```elixir
defmodule Pipeline.Middlewares do
  @moduledoc """
  Common middleware functions demonstrating multi-arity patterns,
  default arguments, and the function capture operator.
  """

  @doc """
  Middleware that adds a timestamp to the context.

  Uses a default argument for the time function, allowing tests
  to inject a fixed time.

  ## Examples

      iex> ctx = Pipeline.Middlewares.timestamp(Pipeline.new_context())
      iex> is_binary(ctx.assigns.timestamp)
      true

  """
  @spec timestamp(Pipeline.context(), (() -> String.t())) :: Pipeline.context()
  def timestamp(context, time_fn \\ &default_timestamp/0) do
    Pipeline.assign(context, :timestamp, time_fn.())
  end

  @doc """
  Creates an authentication middleware configured with a set of valid tokens.

  Returns a function (closure) that captures the `valid_tokens` set.
  This is the factory pattern: a function that returns a middleware function.

  ## Examples

      iex> auth = Pipeline.Middlewares.authenticate(MapSet.new(["secret123"]))
      iex> ctx = Pipeline.new_context(%{token: "secret123"}) |> auth.()
      iex> ctx.assigns.authenticated
      true

      iex> auth = Pipeline.Middlewares.authenticate(MapSet.new(["secret123"]))
      iex> ctx = Pipeline.new_context(%{token: "wrong"}) |> auth.()
      iex> ctx.halted
      true

  """
  @spec authenticate(MapSet.t()) :: Pipeline.middleware()
  def authenticate(valid_tokens) do
    fn context ->
      token = get_in(context, [:assigns, :token])

      if MapSet.member?(valid_tokens, token) do
        Pipeline.assign(context, :authenticated, true)
      else
        context
        |> Pipeline.assign(:authenticated, false)
        |> Map.put(:status, 401)
        |> Map.put(:body, "Unauthorized")
        |> Pipeline.halt_pipeline()
      end
    end
  end

  @doc """
  Creates a rate limiter middleware.

  Takes a maximum request count. The middleware checks
  `assigns.request_count` and halts if exceeded.

  ## Examples

      iex> limiter = Pipeline.Middlewares.rate_limit(100)
      iex> ctx = Pipeline.new_context(%{request_count: 50}) |> limiter.()
      iex> ctx.halted
      false

      iex> limiter = Pipeline.Middlewares.rate_limit(100)
      iex> ctx = Pipeline.new_context(%{request_count: 150}) |> limiter.()
      iex> ctx.halted
      true
      iex> ctx.status
      429

  """
  @spec rate_limit(pos_integer()) :: Pipeline.middleware()
  def rate_limit(max_requests) when is_integer(max_requests) and max_requests > 0 do
    fn context ->
      count = get_in(context, [:assigns, :request_count]) || 0

      if count > max_requests do
        context
        |> Map.put(:status, 429)
        |> Map.put(:body, "Rate limit exceeded")
        |> Pipeline.halt_pipeline()
      else
        context
      end
    end
  end

  @doc """
  Middleware that logs the request (adds a log entry to assigns).

  Uses the function capture operator (&) to reference a named function.
  Demonstrates how `&Module.function/arity` creates a function reference.

  ## Examples

      iex> ctx = Pipeline.new_context() |> Pipeline.Middlewares.logger()
      iex> is_list(ctx.assigns.log)
      true

  """
  @spec logger(Pipeline.context()) :: Pipeline.context()
  def logger(context) do
    log_entry = "Request processed at #{default_timestamp()}"
    existing = Map.get(context.assigns, :log, [])
    Pipeline.assign(context, :log, [log_entry | existing])
  end

  @spec default_timestamp() :: String.t()
  defp default_timestamp do
    DateTime.utc_now() |> DateTime.to_iso8601()
  end
end
```

**Why this works:**

- `timestamp/2` has a default argument (`time_fn \\ &default_timestamp/0`). This
  generates two function heads at compile time: `timestamp/1` (uses default) and
  `timestamp/2` (uses provided function). The default argument makes production code
  simple while allowing tests to inject deterministic timestamps.
- `authenticate/1` returns a function — this is the factory pattern. The returned
  function closes over `valid_tokens`, capturing it at creation time. Each call to
  `authenticate/1` with different tokens produces a different middleware.
- `&default_timestamp/0` is the function capture operator. It converts a named
  function into an anonymous function that can be passed as an argument.
- `rate_limit/1` uses a guard `when is_integer(max_requests) and max_requests > 0`
  to validate configuration at middleware creation time, not at request time.

### Tests

```elixir
# test/pipeline_test.exs
defmodule PipelineTest do
  use ExUnit.Case, async: true

  doctest Pipeline
  doctest Pipeline.Middlewares

  describe "run/2" do
    test "executes middlewares in order" do
      m1 = fn ctx -> Pipeline.assign(ctx, :step1, true) end
      m2 = fn ctx -> Pipeline.assign(ctx, :step2, true) end

      ctx = Pipeline.run([m1, m2], Pipeline.new_context())
      assert ctx.assigns.step1 == true
      assert ctx.assigns.step2 == true
    end

    test "stops at halted middleware" do
      halt = fn ctx -> Pipeline.halt_pipeline(ctx) end
      never = fn _ctx -> raise "should not be called" end

      ctx = Pipeline.run([halt, never], Pipeline.new_context())
      assert ctx.halted == true
    end

    test "empty pipeline returns context unchanged" do
      ctx = Pipeline.new_context(%{original: true})
      assert Pipeline.run([], ctx) == ctx
    end
  end

  describe "full pipeline" do
    test "authentication + rate limiting + timestamp" do
      auth = Pipeline.Middlewares.authenticate(MapSet.new(["valid_token"]))
      limiter = Pipeline.Middlewares.rate_limit(1000)

      fixed_time = fn -> "2024-01-01T00:00:00Z" end
      stamp = fn ctx -> Pipeline.Middlewares.timestamp(ctx, fixed_time) end

      ctx =
        Pipeline.new_context(%{token: "valid_token", request_count: 5})
        |> then(&Pipeline.run([auth, limiter, stamp], &1))

      assert ctx.assigns.authenticated == true
      assert ctx.assigns.timestamp == "2024-01-01T00:00:00Z"
      refute ctx.halted
    end

    test "pipeline halts on bad token" do
      auth = Pipeline.Middlewares.authenticate(MapSet.new(["valid_token"]))
      stamp = fn ctx -> Pipeline.Middlewares.timestamp(ctx) end

      ctx =
        Pipeline.new_context(%{token: "bad_token"})
        |> then(&Pipeline.run([auth, stamp], &1))

      assert ctx.halted
      assert ctx.status == 401
      refute Map.has_key?(ctx.assigns, :timestamp)
    end

    test "pipeline halts on rate limit" do
      auth = Pipeline.Middlewares.authenticate(MapSet.new(["tok"]))
      limiter = Pipeline.Middlewares.rate_limit(10)

      ctx =
        Pipeline.new_context(%{token: "tok", request_count: 100})
        |> then(&Pipeline.run([auth, limiter], &1))

      assert ctx.halted
      assert ctx.status == 429
    end
  end

  describe "function capture" do
    test "named function as middleware using &" do
      # &Pipeline.Middlewares.logger/1 captures the named function
      pipeline = [&Pipeline.Middlewares.logger/1]
      ctx = Pipeline.run(pipeline, Pipeline.new_context())
      assert is_list(ctx.assigns.log)
    end
  end
end
```

### Run the tests

```bash
mix test --trace
```

---

## The function capture operator `&`

```elixir
# Capture a named function
fun = &String.upcase/1
fun.("hello")  # => "HELLO"

# Capture a local function
defmodule Example do
  def double(x), do: x * 2

  def run do
    Enum.map([1, 2, 3], &double/1)  # => [2, 4, 6]
  end
end

# Short syntax for anonymous functions
Enum.map([1, 2, 3], &(&1 * 2))  # => [2, 4, 6]
# &1 refers to the first argument

# Multi-argument short syntax
Enum.reduce([1, 2, 3], 0, &(&1 + &2))  # => 6
# &1 = element, &2 = accumulator
```

The `&` operator is not syntactic sugar — it creates a real function value that can
be stored, passed, and invoked. `&String.upcase/1` is equivalent to
`fn x -> String.upcase(x) end` but more concise.

---

## Default arguments and generated arities

```elixir
def send_email(to, subject, opts \\ [])
# Generates: send_email/2 and send_email/3

def connect(host, port \\ 5432, timeout \\ 5000)
# Generates: connect/1, connect/2, and connect/3
```

Default arguments are filled left to right. `connect("db.example.com", 3306)` sets
`host` to `"db.example.com"` and `port` to `3306`, keeping `timeout` at `5000`.

When a module has multiple clauses with the same name/arity, default arguments must
be declared in a bodyless function head:

```elixir
def process(data, opts \\ [])
def process(data, opts) when is_map(data), do: # ...
def process(data, opts) when is_list(data), do: # ...
```

---

## Common production mistakes

**1. Confusing `fun/1` and `fun/2`**
`Enum.map(list, &process/1)` calls the 1-arity version. If your `process` function
takes 2 arguments, this is a compile error — not a runtime error.

**2. Forgetting the dot for anonymous function calls**
Named functions: `String.upcase("hello")`. Anonymous functions: `fun.("hello")`.
The dot is required for anonymous functions and is not optional style.

**3. Default arguments in multiple clauses**
Defining defaults in more than one clause is a compile error. Use a bodyless head.

**4. Capturing private functions from outside the module**
`&MyModule.private_fun/1` only works inside `MyModule`. From outside, private
functions are invisible. This is by design — it enforces encapsulation.

**5. `&(&1)` is not a function**
`&(&1)` is the identity function (`fn x -> x end`). It works, but it is confusing.
Write `& &1` or `fn x -> x end` for clarity.

---

## Resources

- [Functions — Elixir Getting Started](https://elixir-lang.org/getting-started/modules-and-functions.html)
- [Function captures — HexDocs](https://hexdocs.pm/elixir/Function.html)
- [Default arguments — Elixir Getting Started](https://elixir-lang.org/getting-started/modules-and-functions.html#default-arguments)
- [Plug — HexDocs](https://hexdocs.pm/plug/Plug.html) (the inspiration for this pattern)
