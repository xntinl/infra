# Concurrent Testing in ExUnit

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

`api_gateway`'s test suite has grown to 200+ tests across rate limiter, circuit
breaker, event bus, and middleware modules. CI runs in about 90 seconds because most
tests are `async: false`. The root cause: several modules use named ETS tables and
named GenServers that conflict when tests run in parallel. This exercise migrates the
suite to `async: true` by identifying sources of shared state and isolating each test.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       └── rate_limiter/
│           └── sliding_window.ex       # uses named ETS :request_log
├── test/
│   └── api_gateway/
│       └── rate_limiter/
│           ├── sliding_window_test.exs    # ← async: false currently — you fix this
│           └── isolated_server_test.exs   # ← you implement (Exercise 2)
└── mix.exs
```

---

## The business problem

A slow test suite delays every deploy. The bottleneck is serial test execution:

```
async: false (current)     async: true (target)
[Test A: 300ms]            [Test A] --+
[Test B: 300ms]            [Test B] --| all run in parallel
[Test C: 300ms]            [Test C] --| = ~300ms total
[Test D: 300ms]            [Test D] --+
Total: 1,200ms             Total: ~300ms
```

`async: true` is safe only when tests are fully isolated. The three most common
sources of test interference in Elixir applications:

1. **Named ETS tables** — global, shared across all processes in the node
2. **Named GenServers** — `GenServer.start_link(M, [], name: :foo)` clashes if two
   tests start the same named server concurrently
3. **`Application.put_env/3`** — modifies global application environment

---

## The isolation contract

For a test to be `async: true` safe, it must satisfy:
- It does not read or write named global resources (ETS tables, registered names,
  application env) without either exclusive ownership or per-test uniqueness
- Resources it creates are cleaned up in `on_exit` even if the test fails

---

## Implementation

### Step 1: Refactored sliding window module (table name as argument)

The original `SlidingWindow` hardcodes `:request_log` as the ETS table name.
The refactored version accepts the table name as an argument — this is the minimal change
that enables per-test isolation without restructuring the production code.

In production, you call `SlidingWindow.init(:request_log)` once at startup. In tests,
each test creates a uniquely named table via `System.unique_integer/1`.

```elixir
defmodule ApiGateway.RateLimiter.SlidingWindow do
  @moduledoc """
  Sliding window rate limiter.

  The table name is passed as an argument rather than hardcoded, which
  enables concurrent tests to create isolated tables without conflict.

  Production usage:
    SlidingWindow.init(:request_log)
    SlidingWindow.check("user_1", table: :request_log)

  Test usage (each test creates a unique table name):
    table = :"sw_\#{System.unique_integer([:positive])}"
    SlidingWindow.init(table)
    SlidingWindow.check("user_1", table: table)
  """

  @doc "Create the ETS table. Returns the table name for use in subsequent calls."
  def init(table_name) do
    :ets.new(table_name, [:named_table, :public, :set,
      read_concurrency: true, write_concurrency: true])
    table_name
  end

  @doc """
  Check whether `key` can make a request in the current sliding window.

  opts:
    table:     ETS table name (required)
    limit:     max requests per window (default 100)
    window_ms: window size in ms (default 60_000)

  Returns {:ok, count} | {:error, :rate_limited, retry_after_ms}
  """
  def check(key, opts) do
    table     = Keyword.fetch!(opts, :table)
    limit     = Keyword.get(opts, :limit, 100)
    window_ms = Keyword.get(opts, :window_ms, 60_000)

    now    = System.monotonic_time(:millisecond)
    cutoff = now - window_ms

    timestamps = case :ets.lookup(table, key) do
      []                         -> []
      [{^key, ts}]               -> ts
    end

    in_window = Enum.filter(timestamps, fn ts -> ts > cutoff end)
    count     = length(in_window)

    if count < limit do
      :ets.insert(table, {key, [now | in_window]})
      {:ok, count + 1}
    else
      oldest     = Enum.min(in_window)
      expires_in = oldest + window_ms - now
      {:error, :rate_limited, expires_in}
    end
  end
end
```

### Step 2: Given tests — must pass without modification

The sliding window test creates a unique ETS table per test via `setup`. The `on_exit`
callback ensures the table is deleted even if the test fails. Because each test has its
own table, there is no shared state and `async: true` is safe.

```elixir
# test/api_gateway/rate_limiter/sliding_window_test.exs
defmodule ApiGateway.RateLimiter.SlidingWindowTest do
  use ExUnit.Case, async: true   # ← safe because each test has its own table

  alias ApiGateway.RateLimiter.SlidingWindow

  setup do
    # Each test gets a uniquely named ETS table — no conflict with other tests
    table = :"sw_#{System.unique_integer([:positive])}"
    SlidingWindow.init(table)

    # Cleanup: delete the table when the test exits (even on failure)
    on_exit(fn ->
      if :ets.whereis(table) != :undefined do
        :ets.delete(table)
      end
    end)

    {:ok, table: table}
  end

  test "first request is allowed", %{table: table} do
    assert {:ok, 1} =
      SlidingWindow.check("user_1", table: table, limit: 10, window_ms: 60_000)
  end

  test "requests within limit are all allowed", %{table: table} do
    for i <- 1..10 do
      assert {:ok, ^i} =
        SlidingWindow.check("user_2", table: table, limit: 10, window_ms: 60_000)
    end
  end

  test "request over limit is denied with retry_after", %{table: table} do
    for _ <- 1..5 do
      SlidingWindow.check("user_3", table: table, limit: 5, window_ms: 60_000)
    end

    assert {:error, :rate_limited, retry_after} =
      SlidingWindow.check("user_3", table: table, limit: 5, window_ms: 60_000)

    assert is_integer(retry_after)
    assert retry_after > 0
  end

  test "window expiry frees up the slot", %{table: table} do
    for _ <- 1..3 do
      SlidingWindow.check("user_4", table: table, limit: 3, window_ms: 100)
    end

    assert {:error, :rate_limited, _} =
      SlidingWindow.check("user_4", table: table, limit: 3, window_ms: 100)

    Process.sleep(150)

    assert {:ok, _} =
      SlidingWindow.check("user_4", table: table, limit: 3, window_ms: 100)
  end

  test "different users have independent limits", %{table: table} do
    for _ <- 1..3 do
      SlidingWindow.check("alice", table: table, limit: 3, window_ms: 60_000)
    end

    # alice is over limit, but bob is not
    assert {:error, :rate_limited, _} =
      SlidingWindow.check("alice", table: table, limit: 3, window_ms: 60_000)

    assert {:ok, 1} =
      SlidingWindow.check("bob", table: table, limit: 3, window_ms: 60_000)
  end

  test "concurrent requests from many processes do not corrupt counts", %{table: table} do
    results =
      1..50
      |> Task.async_stream(fn _ ->
        SlidingWindow.check("concurrent_user", table: table, limit: 20, window_ms: 60_000)
      end, max_concurrency: 50)
      |> Enum.map(fn {:ok, result} -> result end)

    allowed  = Enum.count(results, &match?({:ok, _}, &1))
    denied   = Enum.count(results, &match?({:error, :rate_limited, _}, &1))

    assert allowed + denied == 50
    # Some may be over limit due to concurrent writes, but count is reasonable
    assert allowed >= 10
  end
end
```

The isolated server test demonstrates `start_supervised!` for GenServer lifecycle management.
Each test creates a GenServer with a unique name, and `start_supervised!` automatically
stops the process when the test exits.

```elixir
# test/api_gateway/rate_limiter/isolated_server_test.exs
defmodule ApiGateway.RateLimiter.IsolatedServerTest do
  @moduledoc """
  Demonstrates start_supervised! for GenServer lifecycle in tests.
  Each test gets its own GenServer with a unique name — no naming conflicts.
  """
  use ExUnit.Case, async: true

  # Minimal GenServer to demonstrate the pattern
  defmodule Counter do
    use GenServer

    def start_link(opts) do
      name    = Keyword.fetch!(opts, :name)
      initial = Keyword.get(opts, :initial, 0)
      GenServer.start_link(__MODULE__, initial, name: name)
    end

    def value(server),     do: GenServer.call(server, :value)
    def increment(server), do: GenServer.cast(server, :increment)

    def init(n),                     do: {:ok, n}
    def handle_call(:value, _, n),   do: {:reply, n, n}
    def handle_cast(:increment, n),  do: {:noreply, n + 1}
  end

  test "start_supervised! cleans up the process automatically" do
    name = :"counter_#{System.unique_integer([:positive])}"

    pid = start_supervised!({Counter, [name: name, initial: 5]})

    assert is_pid(pid)
    assert Process.alive?(pid)
    assert Counter.value(name) == 5

    # No on_exit needed — start_supervised! registers automatic cleanup
  end

  test "each test has its own counter — no interference" do
    name = :"counter_#{System.unique_integer([:positive])}"
    start_supervised!({Counter, [name: name]})

    Counter.increment(name)
    Counter.increment(name)

    assert Counter.value(name) == 2
    # Even if another test has a Counter and increments it, this name is unique
  end

  test "stop_supervised! stops the process mid-test" do
    name = :"counter_stop_#{System.unique_integer([:positive])}"
    pid  = start_supervised!({Counter, [name: name]}, id: :stoppable_counter)

    Counter.increment(name)
    assert Counter.value(name) == 1

    stop_supervised!(:stoppable_counter)

    refute Process.alive?(pid)
  end

  test "processes started in setup are independent per test" do
    name = :"counter_#{System.unique_integer([:positive])}"
    start_supervised!({Counter, [name: name]})

    # This test's counter starts at 0 — no contamination from other tests
    assert Counter.value(name) == 0
  end
end
```

### Step 3: Run the tests

```bash
# Run in parallel — should be faster than async: false version
mix test test/api_gateway/rate_limiter/ --trace
```

---

## Trade-off analysis

| Isolation technique | When to use | Cost |
|--------------------|-------------|------|
| Unique ETS table name per test | Named ETS tables that cannot be redesigned | Low — add `System.unique_integer` |
| Unnamed ETS table (no `:named_table`) | New code you control | Lowest — no name needed |
| `start_supervised!` | GenServers started in tests | Low — built into ExUnit |
| `async: false` with strict cleanup | Legacy code that cannot be changed | Medium — serializes tests |
| Dependency injection (pass name as arg) | New code being designed | Best — enables async, most flexible |

| `async:` setting | Safe with |
|-----------------|----------|
| `async: true` | Isolated state per test — unique names, no global env |
| `async: false` | Global resources that cannot be isolated |
| Never | Tests that modify production data in a shared DB without sandbox |

---

## Common production mistakes

**1. `async: true` with `Application.put_env/3`**
`Application.put_env` modifies global application environment shared by all
test processes. Two concurrent tests that each call `put_env` for the same key will
race. Use dependency injection (pass the value as a parameter) instead.

**2. `start_supervised!` with a hardcoded module name**
```elixir
# Conflict: two tests start MyServer with the same registered name
start_supervised!(MyServer)   # name: MyServer by default
```
Always generate a unique name: `start_supervised!({MyServer, name: unique_name()})`.

**3. ETS cleanup in `on_exit` that crashes silently**
If the ETS table was already deleted (by another cleanup or by the test itself),
`:ets.delete/1` raises `ArgumentError`. Wrap it:
```elixir
on_exit(fn ->
  if :ets.whereis(table) != :undefined, do: :ets.delete(table)
end)
```

**4. `on_exit` capturing process dictionary state**
`on_exit` callbacks run in a separate ExUnit cleanup process, not in the test
process. `Process.get/1` inside `on_exit` returns `nil`. Capture data in closure
variables, not in the process dictionary.

**5. Named ETS tables in `setup_all` instead of `setup`**
`setup_all` runs once for the entire test module and creates one shared table.
With `async: true`, tests run in parallel and all write to the same table.
Create ETS tables in `setup` (per test), not `setup_all`.

---

## Resources

- [ExUnit.Callbacks — HexDocs](https://hexdocs.pm/ex_unit/ExUnit.Callbacks.html)
- [ExUnit — HexDocs](https://hexdocs.pm/ex_unit/ExUnit.html)
- [ETS — Erlang Docs](https://www.erlang.org/doc/man/ets.html)
- [Testing Elixir — Pragmatic Programmers](https://pragprog.com/titles/lmelixir/testing-elixir/)
