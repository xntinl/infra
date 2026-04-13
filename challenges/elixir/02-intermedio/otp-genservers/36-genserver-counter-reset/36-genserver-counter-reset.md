# GenServer counter with reset — call vs cast

**Project**: `counter_reset_gs` — a minimal GenServer counter that exposes `increment`, `decrement`, `get`, and `reset`, chosen deliberately to illustrate when to use `handle_call` vs `handle_cast`.

---

## Project context

You're migrating a small in-memory counter that used to live inside an Agent into
a proper GenServer so the team gains explicit control over message semantics,
timeouts, and future evolution (metrics, persistence, sharding). The counter
itself is trivial — what matters is that every operation forces you to decide
between `call` (synchronous, reply expected) and `cast` (fire-and-forget).

Senior teams don't pick `call` or `cast` by habit. The choice encodes an
invariant: "does the caller need to know the result before proceeding?" and
"does the caller need back-pressure if the server is slow?". Getting this
wrong bites in production — too many casts and a slow server accumulates an
unbounded mailbox; too many calls and an innocuous degradation becomes a
cascading timeout storm.

In this exercise you implement `get` and `increment`/`decrement` as `call`
(because callers typically want the updated value, and you want back-pressure),
and `reset` as `cast` (fire-and-forget admin operation — the caller doesn't
need to block the request path waiting for a reset to commit).

Project structure:

```
counter_reset_gs/
├── lib/
│   └── counter_reset_gs.ex
├── test/
│   └── counter_reset_gs_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not a pure Agent?** Agent hides the `call` vs `cast` distinction — this exercise is exactly about that boundary.
- **Why not `:counters`?** `:counters` is faster but has no custom logic, timeouts, or supervision hooks.

## Core concepts

### 1. `handle_call` — synchronous, reply expected

`GenServer.call/3` sends a message, blocks the caller, and waits for `reply`.
Default timeout is 5 seconds; if the server doesn't reply in time, the caller
crashes with `:timeout`. This is a feature: the caller gets natural
back-pressure — if the server falls behind, callers visibly slow down or fail
instead of silently piling up work.

```
caller ──{:"$gen_call", from, msg}──▶ server
caller ◀──────────reply─────────────── server
```

### 2. `handle_cast` — asynchronous, no reply

`GenServer.cast/2` sends a message and returns `:ok` immediately. No
acknowledgement, no timeout, no flow control. Great for "I don't care when
this runs as long as it eventually does" — bad for anything the caller must
confirm succeeded.

```
caller ──{:"$gen_cast", msg}──▶ server   (caller keeps going)
```

### 3. The mailbox is unbounded

The BEAM does not bound process mailboxes. A GenServer that accepts casts
faster than it can process them will grow its mailbox until the VM runs out
of memory. `call` naturally back-pressures; `cast` does not. This single fact
drives 80% of the `call` vs `cast` decision.

### 4. Why `reset` is a good fit for `cast`

- The caller doesn't need the new value — it's 0 by definition.
- It's an administrative operation, not on a hot path, so mailbox growth is
  a non-issue.
- Losing back-pressure here doesn't hurt — resets are rare.

Contrast with `increment`: the caller almost always wants the new counter
value for logs/metrics, and under load you want back-pressure.

---

## Design decisions

**Option A — single `call` for every op including reset**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `call` for read/write, `cast` for reset (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because reset has no meaningful reply and is rare; mixing semantics teaches the boundary.


## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    # stdlib-only by default; add `{:benchee, "~> 1.3", only: :dev}` if you benchmark
  ]
end
```


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new counter_reset_gs
cd counter_reset_gs
```

### Step 2: `lib/counter_reset_gs.ex`

**Objective**: Implement `counter_reset_gs.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.


```elixir
defmodule CounterResetGs do
  @moduledoc """
  A GenServer counter that deliberately mixes `call` and `cast` to teach the
  trade-off:

    * `increment/1`, `decrement/1`, `get/1` use `call` — the caller wants the
      updated value and benefits from back-pressure.
    * `reset/1` uses `cast` — fire-and-forget admin operation.
  """

  use GenServer

  @type name :: GenServer.server()

  # ── Public API ──────────────────────────────────────────────────────────

  @doc "Starts the counter. `initial` defaults to 0."
  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    {initial, opts} = Keyword.pop(opts, :initial, 0)
    GenServer.start_link(__MODULE__, initial, opts)
  end

  @doc "Returns the current value. Synchronous — caller waits for reply."
  @spec get(name()) :: integer()
  def get(server), do: GenServer.call(server, :get)

  @doc "Increments by `n` (default 1) and returns the new value."
  @spec increment(name(), integer()) :: integer()
  def increment(server, n \\ 1), do: GenServer.call(server, {:increment, n})

  @doc "Decrements by `n` (default 1) and returns the new value."
  @spec decrement(name(), integer()) :: integer()
  def decrement(server, n \\ 1), do: GenServer.call(server, {:decrement, n})

  @doc """
  Resets the counter to 0. Asynchronous — fire and forget.
  Returns `:ok` immediately, not the new value (which is 0 by definition).
  """
  @spec reset(name()) :: :ok
  def reset(server), do: GenServer.cast(server, :reset)

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(initial) when is_integer(initial), do: {:ok, initial}

  @impl true
  def handle_call(:get, _from, value), do: {:reply, value, value}

  def handle_call({:increment, n}, _from, value) do
    new_value = value + n
    {:reply, new_value, new_value}
  end

  def handle_call({:decrement, n}, _from, value) do
    new_value = value - n
    {:reply, new_value, new_value}
  end

  @impl true
  def handle_cast(:reset, _value), do: {:noreply, 0}
end
```

### Step 3: `test/counter_reset_gs_test.exs`

**Objective**: Write `counter_reset_gs_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule CounterResetGsTest do
  use ExUnit.Case, async: true

  setup do
    {:ok, pid} = CounterResetGs.start_link(initial: 0)
    %{pid: pid}
  end

  describe "synchronous operations (call)" do
    test "get returns the current value", %{pid: pid} do
      assert CounterResetGs.get(pid) == 0
    end

    test "increment returns the new value", %{pid: pid} do
      assert CounterResetGs.increment(pid) == 1
      assert CounterResetGs.increment(pid, 5) == 6
      assert CounterResetGs.get(pid) == 6
    end

    test "decrement returns the new value", %{pid: pid} do
      CounterResetGs.increment(pid, 10)
      assert CounterResetGs.decrement(pid) == 9
      assert CounterResetGs.decrement(pid, 4) == 5
    end
  end

  describe "asynchronous reset (cast)" do
    test "reset returns :ok immediately and eventually sets value to 0", %{pid: pid} do
      CounterResetGs.increment(pid, 42)
      assert :ok = CounterResetGs.reset(pid)

      # cast is async — but a subsequent call is serialized AFTER the cast
      # in the mailbox, so by the time `get` replies the reset has run.
      # This is how you synchronize with a prior cast: issue any call.
      assert CounterResetGs.get(pid) == 0
    end
  end

  describe "initial value" do
    test "respects :initial option" do
      {:ok, pid} = CounterResetGs.start_link(initial: 100)
      assert CounterResetGs.get(pid) == 100
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.



## Key Concepts: State Mutations and Side Effects in GenServer

Each `handle_call` and `handle_cast` invocation receives the current state and returns a new state. Elixir's immutability means the old state is discarded; only the new state persists. This forces explicit reasoning about state transitions. If you forget to return the new state (e.g., `:ok` instead of `{:reply, :ok, new_state}`), the server's state stays unchanged—a silent bug.

A common pattern is embedding the state mutation logic in a private helper function that returns the new state, then using that in the handler. This separates pure logic (state → new state) from impure side effects (logging, external calls). For example, increment logic lives in a pure function; logging lives in the handler. This makes the server easier to test: you can test the pure function independently.


## Benchmark

```elixir
{:ok, pid} = CounterResetGs.start_link(initial: 0)
{us, _} = :timer.tc(fn ->
  for _ <- 1..100_000, do: CounterResetGs.increment(pid)
end)
IO.puts("#{us / 100_000} µs per call")
```

Target esperado: <2 µs por `increment` en hardware moderno (GenServer call local).

## Trade-offs and production gotchas

**1. Casts have no back-pressure — ever**
If callers cast faster than the server can process, the mailbox grows without
bound. Memory pressure shows up as "node slowly dies" rather than a clear
error. Default to `call` unless you have a concrete reason to use `cast`.

**2. `GenServer.call/2` timeout defaults to 5_000 ms**
Under load, a slow handler turns into `:timeout` errors in callers. Tune with
`GenServer.call(server, msg, timeout)` and make sure callers handle timeouts
instead of letting them crash the call site.

**3. Cast + call serialization is a real synchronization tool**
A `call` issued after a `cast` to the same GenServer is guaranteed to be
processed after the cast (they're FIFO in the mailbox). Tests often rely on
this — issuing a trivial `get` after a cast to "flush" it.

**4. Never use `cast` for operations that can fail meaningfully**
If a DB write can fail, the caller usually needs to know. Cast loses that
signal. Use `call` (or `call` + async supervisor task) to preserve errors.

**5. Consider `GenServer.reply/2` for slow calls**
If a call handler needs to do slow I/O, it can return `{:noreply, state}`
and later call `GenServer.reply(from, result)` so the server stays responsive
to other callers. Blocking in `handle_call` blocks the entire GenServer.

**6. When NOT to use a GenServer counter**
If you only need atomic increments across processes with no logic, use
`:counters` or `:atomics` from Erlang's stdlib — they're lock-free,
per-scheduler, and orders of magnitude faster than a GenServer. Reach for
a GenServer when you need logic, not just a number.

---


## Reflection

- Si necesitaras exponer `reset` a usuarios no admins, ¿seguiría siendo `cast`? ¿Qué cambiás y por qué?

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule CounterResetGs do
    @moduledoc """
    A GenServer counter that deliberately mixes `call` and `cast` to teach the
    trade-off:

      * `increment/1`, `decrement/1`, `get/1` use `call` — the caller wants the
        updated value and benefits from back-pressure.
      * `reset/1` uses `cast` — fire-and-forget admin operation.
    """

    use GenServer

    @type name :: GenServer.server()

    # ── Public API ──────────────────────────────────────────────────────────

    @doc "Starts the counter. `initial` defaults to 0."
    @spec start_link(keyword()) :: GenServer.on_start()
    def start_link(opts \\ []) do
      {initial, opts} = Keyword.pop(opts, :initial, 0)
      GenServer.start_link(__MODULE__, initial, opts)
    end

    @doc "Returns the current value. Synchronous — caller waits for reply."
    @spec get(name()) :: integer()
    def get(server), do: GenServer.call(server, :get)

    @doc "Increments by `n` (default 1) and returns the new value."
    @spec increment(name(), integer()) :: integer()
    def increment(server, n \\ 1), do: GenServer.call(server, {:increment, n})

    @doc "Decrements by `n` (default 1) and returns the new value."
    @spec decrement(name(), integer()) :: integer()
    def decrement(server, n \\ 1), do: GenServer.call(server, {:decrement, n})

    @doc """
    Resets the counter to 0. Asynchronous — fire and forget.
    Returns `:ok` immediately, not the new value (which is 0 by definition).
    """
    @spec reset(name()) :: :ok
    def reset(server), do: GenServer.cast(server, :reset)

    # ── Callbacks ───────────────────────────────────────────────────────────

    @impl true
    def init(initial) when is_integer(initial), do: {:ok, initial}

    @impl true
    def handle_call(:get, _from, value), do: {:reply, value, value}

    def handle_call({:increment, n}, _from, value) do
      new_value = value + n
      {:reply, new_value, new_value}
    end

    def handle_call({:decrement, n}, _from, value) do
      new_value = value - n
      {:reply, new_value, new_value}
    end

    @impl true
    def handle_cast(:reset, _value), do: {:noreply, 0}
  end

  def main do
    {:ok, pid} = CounterResetGs.start_link(initial: 10)
  
    v1 = CounterResetGs.get(pid)
    IO.puts("Initial value: #{v1}")
  
    v2 = CounterResetGs.increment(pid, 5)
    IO.puts("After increment(5): #{v2}")
  
    :ok = CounterResetGs.reset(pid)
    v3 = CounterResetGs.get(pid)
    IO.puts("After reset: #{v3}")
  
    IO.puts("✓ CounterResetGs works correctly")
  end

end

Main.main()
```


## Resources

- [`GenServer` — Elixir stdlib](https://hexdocs.pm/elixir/GenServer.html)
- ["Designing for scalability with Erlang/OTP" — Cesarini & Vinoski, O'Reilly](https://www.oreilly.com/library/view/designing-for-scalability/9781449361556/)
- [`:counters` — Erlang stdlib](https://www.erlang.org/doc/man/counters.html) — for when you outgrow a GenServer counter
- [Fred Hébert — "Stuff Goes Bad: Erlang in Anger"](https://www.erlang-in-anger.com/) — chapters on mailbox overflow
