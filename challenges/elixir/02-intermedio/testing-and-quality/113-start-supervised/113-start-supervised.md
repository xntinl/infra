# `start_supervised!/1` for clean test fixtures

**Project**: `start_supervised_demo` — a `Counter` GenServer tested with
`start_supervised!/1` instead of ad-hoc `GenServer.start_link/3`, ensuring
automatic cleanup between tests.

---

## Project context

The manual-cleanup pattern — `start_link` + `on_exit(fn -> stop end)` —
is error-prone: you forget the `on_exit`, the process leaks, and the next
test flakes "intermittently" because the old one is still alive. ExUnit
provides `start_supervised!/1`: it starts your child under ExUnit's own
supervisor, scoped to the test, and tears it down automatically when the
test finishes.

If you're writing tests against GenServers, Agents, Tasks, or
`DynamicSupervisor`-style code, `start_supervised!/1` should be your
default. The only reason to use raw `start_link` is when you're explicitly
testing the startup/crash behavior.

## Why `start_supervised!/1` and not X

**Why not `start_link/1` + `on_exit(fn -> stop end)`?** It's two code paths
that must stay in sync — one to start, one to clean up. Forget either and
tests leak or fail cryptically. `start_supervised!` is one line with correct
teardown baked in.

**Why not `Application.start/1` or a real Application supervisor?** Because
those are process-global and shared across tests. You'd lose `async: true`
and test isolation together.

**Why the bang version?** Because failure at setup should abort the test
with a clear reason. `start_supervised/1` (no bang) returns `{:error, _}`,
which means silent tests when setup fails.

Project structure:

```
start_supervised_demo/
├── lib/
│   └── counter.ex
├── test/
│   ├── counter_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Core concepts

### 1. `start_supervised!/1` vs `start_link/1`

```elixir
# Old pattern — you own cleanup:
{:ok, pid} = Counter.start_link(name: :my_counter)
on_exit(fn -> GenServer.stop(pid) end)

# New pattern — ExUnit owns cleanup:
pid = start_supervised!(Counter)
```

`start_supervised!/1` takes a child spec (a module, `{Module, arg}`, or a
full spec) and returns the pid. If start fails, it raises (the `!`),
failing the test immediately with a clear message. No `on_exit`, no leak.

### 2. ExUnit's per-test supervisor

Under the hood, ExUnit starts a dedicated supervisor for each test. Every
child you start via `start_supervised` / `start_link_supervised!` is a
child of that supervisor. When the test ends, ExUnit shuts down the
supervisor — which terminates every child in reverse start order. Clean.

### 3. `start_link_supervised!/1` for linking

If you want the TEST PROCESS to crash when the child crashes (sometimes
useful to surface unexpected exits), use `start_link_supervised!/1`.
Default: `start_supervised!/1` does NOT link, so a child crash doesn't
take the test out.

### 4. Name conflicts still apply

`start_supervised!({Counter, name: :global_name})` across two async tests
will collide, because the name is global. Either:
- Use `async: false`.
- Pass per-test unique names.
- Use `:via` registries keyed by the test pid.

---

## Design decisions

**Option A — `start_supervised!/1` (no link)** (chosen default)
- Pros: Child crash doesn't take out the test, so unrelated assertions
  can still execute and report cleanly.
- Cons: A silent crash can hide a bug unless you also monitor the pid.

**Option B — `start_link_supervised!/1`**
- Pros: Crashes propagate to the test process; no silent failures.
- Cons: One buggy child fails every other assertion in the test.

→ Chose **A as the default**. Pair with `Process.monitor/1` +
`assert_receive {:DOWN, ...}` when crash detection is the test's point.
Reach for **B** only when an unexpected crash should end the test.

---

## Implementation

### Step 1: Create the project

```bash
mix new start_supervised_demo
cd start_supervised_demo
```

### Step 2: `lib/counter.ex`

```elixir
defmodule Counter do
  @moduledoc """
  Minimal GenServer counter — used to demonstrate `start_supervised!/1`.
  """
  use GenServer

  # ── Public API ─────────────────────────────────────────────────────────

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    {initial, opts} = Keyword.pop(opts, :initial, 0)
    GenServer.start_link(__MODULE__, initial, opts)
  end

  @spec bump(GenServer.server(), pos_integer()) :: :ok
  def bump(server, by \\ 1), do: GenServer.cast(server, {:bump, by})

  @spec value(GenServer.server()) :: integer()
  def value(server), do: GenServer.call(server, :value)

  # ── Callbacks ──────────────────────────────────────────────────────────

  @impl true
  def init(initial), do: {:ok, initial}

  @impl true
  def handle_cast({:bump, by}, n), do: {:noreply, n + by}

  @impl true
  def handle_call(:value, _from, n), do: {:reply, n, n}
end
```

### Step 3: `test/counter_test.exs`

```elixir
defmodule CounterTest do
  use ExUnit.Case, async: true

  # One helper used by multiple describe blocks. Keeps each test isolated.
  defp start_counter(opts \\ []) do
    start_supervised!({Counter, opts})
  end

  describe "start_supervised!/1 basics" do
    test "starts a counter and tears it down automatically" do
      counter = start_counter()

      Counter.bump(counter)
      Counter.bump(counter, 4)
      assert Counter.value(counter) == 5

      # No on_exit here — ExUnit kills the counter when the test ends.
      assert Process.alive?(counter)
    end

    test "each test gets a fresh counter (isolation check)" do
      counter = start_counter()
      assert Counter.value(counter) == 0  # fresh, not 5 from the previous test
    end

    test "arg is forwarded to the child spec" do
      counter = start_counter(initial: 42)
      assert Counter.value(counter) == 42
    end
  end

  describe "multiple supervised children" do
    test "children are torn down in reverse start order" do
      a = start_counter(initial: 1)
      b = start_counter(initial: 2)

      # Both live during the test.
      assert Process.alive?(a)
      assert Process.alive?(b)
      assert Counter.value(a) + Counter.value(b) == 3
    end
  end

  describe "fetching supervised children" do
    test "start_supervised returns :ignore/error tuple variant when asked" do
      # start_supervised/1 (no bang) returns {:ok, pid} | :ignore | {:error, _}
      assert {:ok, pid} = start_supervised({Counter, [initial: 7]})
      assert is_pid(pid)
      assert Counter.value(pid) == 7
    end

    test "stop_supervised/1 terminates a child early" do
      pid = start_counter()
      assert Process.alive?(pid)

      assert :ok = stop_supervised(Counter)

      # The process is gone before the test ends.
      refute Process.alive?(pid)
    end
  end
end
```

### Step 4: Run

```bash
mix test
mix test --trace
```

### Why this works

ExUnit spins up a dedicated supervisor per test. Every child started via
`start_supervised[!]` lives under it, and when the test finishes (pass or
fail) ExUnit shuts the supervisor down — terminating all children in
reverse start order, synchronously. That removes the entire class of "I
forgot `on_exit`" leaks and ensures determinism between tests in an
`async: true` suite.

---

## Benchmark

<!-- benchmark N/A: tema de estructura de tests; la única medición
pertinente es "tiempo de teardown por test" y suele ser sub-ms. -->

---

## Trade-offs and production gotchas

**1. `start_supervised!` is a runtime dependency on ExUnit's supervisor**
Which only exists during a test. Don't try to call it from your `lib/`
code — it's a test-only tool.

**2. No link by default — exits go unnoticed**
If a supervised child crashes mid-test, `start_supervised!/1` doesn't
propagate. Your test might continue and make unrelated assertions that
pass, hiding the bug. Use `start_link_supervised!/1` OR
`Process.monitor/1` + `assert_receive {:DOWN, ...}` when crash detection
matters.

**3. Name collisions with async**
A named child (`{Counter, name: Counter}`) from two async tests will
clash on `start_supervised!`. Either make the name per-test or mark the
module `async: false`.

**4. The teardown order is reverse-start**
If child B depends on child A, start A first so A is torn down *after*
B — otherwise B crashes during teardown, logging noise.

**5. When NOT to use `start_supervised!`**
When you're explicitly testing start/crash semantics ("does the supervisor
restart this child after an exit?"). There, you want raw `start_link`
or a test-local supervisor you fully control.

---

## Reflection

- You're testing a GenServer that registers itself as `{:global, :foo}` on
  start. How do you make this compatible with `async: true` tests, and
  what happens if you don't?
- Given two interdependent children (A uses B's pid at init), explain in
  what order you start them with `start_supervised!`, and what goes wrong
  at **teardown** if you get it wrong.

---

## Resources

- [`ExUnit.Callbacks.start_supervised!/2`](https://hexdocs.pm/ex_unit/ExUnit.Callbacks.html#start_supervised!/2)
- [`ExUnit.Callbacks.start_link_supervised!/2`](https://hexdocs.pm/ex_unit/ExUnit.Callbacks.html#start_link_supervised!/2)
- [`ExUnit.Callbacks.stop_supervised/1`](https://hexdocs.pm/ex_unit/ExUnit.Callbacks.html#stop_supervised/1)
- ["Testing GenServers" — Chris Keathley's blog](https://keathley.io/blog/) — the pattern `start_supervised!` + `async: true` popularized in the community
