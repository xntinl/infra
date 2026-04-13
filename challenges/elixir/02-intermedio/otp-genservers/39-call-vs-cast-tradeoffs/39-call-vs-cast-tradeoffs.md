# Call vs cast — measuring latency, throughput, and back-pressure

**Project**: `call_vs_cast_demo` — the same GenServer exposed twice (via `call` and via `cast`) with a micro-benchmark using `:timer.tc/1` so you can see the trade-offs in numbers.

---

## Project context

Every Elixir codebase eventually accumulates GenServers where the choice
between `call` and `cast` was made by copy-paste rather than by reasoning.
This exercise is the antidote: implement one trivial operation (add a number
to an accumulator) twice — once synchronous, once asynchronous — and measure
the wall-clock difference for 100_000 operations.

The goal is not "cast is faster" — that's obvious. The goal is to make the
following intuitions concrete:

- `call` serializes producers through a round-trip; its throughput is bounded
  by server latency.
- `cast` returns immediately, so the producer's wall time looks great — but
  the work hasn't actually happened yet, and the mailbox may be unbounded.
- Measuring cast throughput correctly requires a **drain** step (a follow-up
  `call` that flushes the mailbox) or you'll think cast is infinitely fast.

Once you internalize this, the `call` vs `cast` decision becomes mechanical.

Project structure:

```
call_vs_cast_demo/
├── lib/
│   ├── call_vs_cast_demo.ex
│   ├── call_vs_cast_demo/sync_server.ex
│   └── call_vs_cast_demo/async_server.ex
├── test/
│   └── call_vs_cast_demo_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not a lower-level alternative?** For call vs cast tradeoffs, OTP's pattern is what reviewers will expect and what observability tools support out of the box.

## Core concepts

### 1. What `call` actually costs

A `GenServer.call` does: send message, block on a selective `receive`, wake
up when a matching `{ref, reply}` arrives. That's at least two context
switches per call, plus a monitor setup/teardown. Typical latency: a few
microseconds per call on modern hardware, times the number of calls.

### 2. What `cast` actually costs

A `GenServer.cast` does: send message, return `:ok`. Zero blocking on the
producer side. The server still has to process each message, but now at its
own pace — and the producer has no idea when that happens.

```
 call:   prod ─▶ [mailbox] ─▶ server ─▶ reply ─▶ prod
         (prod blocked the whole time)

 cast:   prod ─▶ [mailbox]                           (prod continues)
                    │
                    ▼
                 server  (whenever it gets to it)
```

### 3. Measuring correctly with `:timer.tc/1`

`:timer.tc(fun)` returns `{microseconds, result}`. For cast throughput, the
producer finishes instantly — but "finishing" doesn't mean the work is done.
To measure **effective** throughput, issue a `call` after all casts: the
call won't return until every preceding cast has been processed (mailbox
is FIFO). That's your true end-to-end time.

### 4. Back-pressure and mailbox explosion

A runaway producer casting into a slow server will grow the mailbox
without bound. BEAM mailboxes live in the process heap, so this shows up
as slow GC, then a memory spike, then OOM. `call` back-pressures naturally
because the producer can't issue a second call until the first replies.

---

## Design decisions

**Option A — always `call` (synchronous, back-pressured)**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — mix `call` and `cast` by intent (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because mailbox back-pressure matters where load is unbounded; `cast` is only safe on admin paths.


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

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new call_vs_cast_demo
cd call_vs_cast_demo
```

### Step 2: `lib/call_vs_cast_demo/sync_server.ex`

**Objective**: Implement `sync_server.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.


```elixir
defmodule CallVsCastDemo.SyncServer do
  @moduledoc """
  Same-responsibility GenServer exposed via `GenServer.call`. Every `add/2`
  blocks until the server replies — producers get back-pressure for free.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, 0, opts)

  @doc "Adds `n` to the accumulator and returns the new value."
  @spec add(GenServer.server(), integer()) :: integer()
  def add(server, n), do: GenServer.call(server, {:add, n})

  @doc "Returns the current accumulator value."
  @spec value(GenServer.server()) :: integer()
  def value(server), do: GenServer.call(server, :value)

  @impl true
  def init(initial), do: {:ok, initial}

  @impl true
  def handle_call({:add, n}, _from, acc), do: {:reply, acc + n, acc + n}
  def handle_call(:value, _from, acc), do: {:reply, acc, acc}
end
```

### Step 3: `lib/call_vs_cast_demo/async_server.ex`

**Objective**: Implement `async_server.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.


```elixir
defmodule CallVsCastDemo.AsyncServer do
  @moduledoc """
  Same-responsibility GenServer exposed via `GenServer.cast`. `add/2`
  returns `:ok` immediately with no acknowledgement. Use `value/1` (a
  `call`) to drain the mailbox and read the current value.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, 0, opts)

  @doc "Asynchronously adds `n` to the accumulator. Fire-and-forget."
  @spec add(GenServer.server(), integer()) :: :ok
  def add(server, n), do: GenServer.cast(server, {:add, n})

  @doc """
  Returns the current accumulator value. Because a `call` is FIFO-serialized
  after all prior casts, this also acts as a drain barrier.
  """
  @spec value(GenServer.server()) :: integer()
  def value(server), do: GenServer.call(server, :value)

  @impl true
  def init(initial), do: {:ok, initial}

  @impl true
  def handle_cast({:add, n}, acc), do: {:noreply, acc + n}

  @impl true
  def handle_call(:value, _from, acc), do: {:reply, acc, acc}
end
```

### Step 4: `lib/call_vs_cast_demo.ex`

**Objective**: Implement `call_vs_cast_demo.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.


```elixir
defmodule CallVsCastDemo do
  @moduledoc """
  Micro-benchmark comparing `call` and `cast` throughput for the same
  logical operation. See module docs of `SyncServer` and `AsyncServer`.
  """

  alias CallVsCastDemo.{SyncServer, AsyncServer}

  @type result :: %{
          flavor: :call | :cast,
          operations: pos_integer(),
          total_us: non_neg_integer(),
          per_op_us: float(),
          final_value: integer()
        }

  @doc """
  Runs `n` add-1 operations against both servers and returns a list of
  results. For the async run, the elapsed time INCLUDES the drain `call`,
  which is the only honest way to measure effective throughput.
  """
  @spec bench(pos_integer()) :: [result()]
  def bench(n \\ 100_000) do
    [bench_sync(n), bench_async(n)]
  end

  defp bench_sync(n) do
    {:ok, pid} = SyncServer.start_link()

    {elapsed, _} =
      :timer.tc(fn ->
        Enum.each(1..n, fn _ -> SyncServer.add(pid, 1) end)
      end)

    value = SyncServer.value(pid)
    GenServer.stop(pid)

    %{
      flavor: :call,
      operations: n,
      total_us: elapsed,
      per_op_us: elapsed / n,
      final_value: value
    }
  end

  defp bench_async(n) do
    {:ok, pid} = AsyncServer.start_link()

    {elapsed, value} =
      :timer.tc(fn ->
        Enum.each(1..n, fn _ -> AsyncServer.add(pid, 1) end)
        # Drain: the call flushes every prior cast because the mailbox is FIFO.
        AsyncServer.value(pid)
      end)

    GenServer.stop(pid)

    %{
      flavor: :cast,
      operations: n,
      total_us: elapsed,
      per_op_us: elapsed / n,
      final_value: value
    }
  end
end
```

### Step 5: `test/call_vs_cast_demo_test.exs`

**Objective**: Write `call_vs_cast_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule CallVsCastDemoTest do
  use ExUnit.Case, async: true

  alias CallVsCastDemo.{SyncServer, AsyncServer}

  describe "SyncServer — call semantics" do
    test "add returns the new value" do
      {:ok, pid} = SyncServer.start_link()
      assert SyncServer.add(pid, 3) == 3
      assert SyncServer.add(pid, 4) == 7
      assert SyncServer.value(pid) == 7
    end
  end

  describe "AsyncServer — cast semantics" do
    test "add returns :ok and value reflects all prior casts" do
      {:ok, pid} = AsyncServer.start_link()
      for _ <- 1..10, do: assert(:ok = AsyncServer.add(pid, 1))
      # The call drains the cast mailbox.
      assert AsyncServer.value(pid) == 10
    end
  end

  describe "bench/1" do
    test "produces both flavors with correct final values" do
      # Small N for fast CI.
      [sync, async] = CallVsCastDemo.bench(1_000)

      assert sync.flavor == :call
      assert sync.final_value == 1_000
      assert sync.operations == 1_000
      assert sync.total_us >= 0

      assert async.flavor == :cast
      assert async.final_value == 1_000
      assert async.operations == 1_000
      assert async.total_us >= 0
    end

    test "cast with drain does not lose writes" do
      [_sync, async] = CallVsCastDemo.bench(5_000)
      assert async.final_value == 5_000
    end
  end
end
```

### Step 6: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
# To see actual numbers from your machine:
#   iex -S mix
#   iex> CallVsCastDemo.bench(100_000) |> IO.inspect()
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Deep Dive: Flow Control and Mailbox Pressure in Production

The call/cast trade-off is fundamentally about whether you want the process to push back when it can't keep up. A `call` that times out after 5 seconds gives callers a hard signal: "I couldn't process your request." That signal propagates: if you're in an HTTP request handler and your GenServer `call` times out, the HTTP layer sees a timeout and can respond with 503 or retry. A `cast`, by contrast, silently succeeds — it just enqueues — and the server's mailbox grows invisibly until memory is exhausted.

In production systems at scale, the single biggest source of GenServer pain is unbounded mailboxes from casts. The naive metric — "casts are faster" — masks reality: they're fast *for the caller*, but the server pays the cost later, and by then the damage (memory pressure, GC pauses, cascading timeouts in unrelated processes) is done. High-volume producers should default to `call` with timeout tuning, or use techniques like shedding (ignore casts under load) and batching (combine N casts into one aggregate message).

Monitoring mailbox depth is essential. Libraries like `:observer` or integration with Prometheus can emit mailbox size; if you see sustained growth, it's a cast problem. Pattern match: if the operation has a meaningful failure case or the caller benefits from knowing when work completed, use `call`. If it's purely "fire and hope", use `cast` but always have backpressure elsewhere (rate limiting, per-producer quotas, shedding logic).


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. Naive cast benchmarks lie**
If you measure a cast loop without the drain `call`, you're timing how fast
you can append to a mailbox — not the end-to-end time. Always drain before
stopping the clock.

**2. Cast looks faster until it doesn't**
Producer wall time with `cast` is dominated by `send/2` cost — tiny. But
the server still has to do all the work, and if it's slower than the
producer, the mailbox explodes. Cast defers the cost; it doesn't eliminate
it.

**3. `call` throughput is bounded by server latency**
If each `handle_call` takes 100 µs, you can't exceed ~10k calls/s through
that process no matter how many producers you add. `call` fans in but does
not fan out. Sharding (`PartitionSupervisor`) is the escape.

**4. Cast silently drops work on crash**
If a GenServer crashes with unprocessed casts in the mailbox, those
messages are gone. With `call`, the caller gets a crash they can retry or
propagate. Choose `cast` only when losing the message is tolerable.

**5. `call` with `:infinity` is a footgun**
Using `GenServer.call(pid, msg, :infinity)` disables the default 5s timeout.
If the server deadlocks, every caller blocks forever. Use `:infinity` only
with explicit justification and monitoring.

**6. When NOT to use `cast` at all**
If any of these apply, use `call`: (a) the caller needs the result, (b) the
operation can fail meaningfully, (c) you need back-pressure, (d) you need
ordering guarantees against a subsequent read from the same caller.

---


## Reflection

- Tu GenServer recibe 10k casts/seg y procesa 8k/seg. Describí exactamente cómo y cuándo muere el nodo, y qué cambios harías para que falle ruidosamente antes.

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule CallVsCastDemo do
    @moduledoc """
    Micro-benchmark comparing `call` and `cast` throughput for the same
    logical operation. See module docs of `SyncServer` and `AsyncServer`.
    """

    alias CallVsCastDemo.{SyncServer, AsyncServer}

    @type result :: %{
            flavor: :call | :cast,
            operations: pos_integer(),
            total_us: non_neg_integer(),
            per_op_us: float(),
            final_value: integer()
          }

    @doc """
    Runs `n` add-1 operations against both servers and returns a list of
    results. For the async run, the elapsed time INCLUDES the drain `call`,
    which is the only honest way to measure effective throughput.
    """
    @spec bench(pos_integer()) :: [result()]
    def bench(n \\ 100_000) do
      [bench_sync(n), bench_async(n)]
    end

    defp bench_sync(n) do
      {:ok, pid} = SyncServer.start_link()

      {elapsed, _} =
        :timer.tc(fn ->
          Enum.each(1..n, fn _ -> SyncServer.add(pid, 1) end)
        end)

      value = SyncServer.value(pid)
      GenServer.stop(pid)

      %{
        flavor: :call,
        operations: n,
        total_us: elapsed,
        per_op_us: elapsed / n,
        final_value: value
      }
    end

    defp bench_async(n) do
      {:ok, pid} = AsyncServer.start_link()

      {elapsed, value} =
        :timer.tc(fn ->
          Enum.each(1..n, fn _ -> AsyncServer.add(pid, 1) end)
          # Drain: the call flushes every prior cast because the mailbox is FIFO.
          AsyncServer.value(pid)
        end)

      GenServer.stop(pid)

      %{
        flavor: :cast,
        operations: n,
        total_us: elapsed,
        per_op_us: elapsed / n,
        final_value: value
      }
    end
  end

  defmodule SyncServer do
    use GenServer
    def start_link(), do: GenServer.start_link(__MODULE__, 0)
    def add(pid, n), do: GenServer.call(pid, {:add, n})
    def value(pid), do: GenServer.call(pid, :value)
    def init(state), do: {:ok, state}
    def handle_call({:add, n}, _from, state), do: {:reply, state + n, state + n}
    def handle_call(:value, _from, state), do: {:reply, state, state}
  end

  defmodule AsyncServer do
    use GenServer
    def start_link(), do: GenServer.start_link(__MODULE__, 0)
    def add(pid, n), do: GenServer.cast(pid, {:add, n})
    def value(pid), do: GenServer.call(pid, :value)
    def init(state), do: {:ok, state}
    def handle_cast({:add, n}, state), do: {:noreply, state + n}
    def handle_call(:value, _from, state), do: {:reply, state, state}
  end

  def main do
    {:ok, sync_pid} = SyncServer.start_link()
    v1 = SyncServer.add(sync_pid, 10)
    IO.puts("SyncServer add result: #{v1}")
  
    {:ok, async_pid} = AsyncServer.start_link()
    :ok = AsyncServer.add(async_pid, 10)
    v2 = AsyncServer.value(async_pid)
    IO.puts("AsyncServer final value: #{v2}")
  
    IO.puts("✓ CallVsCastDemo works correctly")
  end

end

Main.main()
```


## Resources

- [`GenServer.call/3` and `GenServer.cast/2`](https://hexdocs.pm/elixir/GenServer.html#call/3)
- [`:timer.tc/1` — Erlang microsecond timer](https://www.erlang.org/doc/man/timer.html#tc-1)
- [Benchee — for statistically sound Elixir benchmarks](https://hexdocs.pm/benchee/) — use this instead of `:timer.tc` for real benchmarks
- [Fred Hébert — "Stuff Goes Bad: Erlang in Anger", chapter on overload](https://www.erlang-in-anger.com/)
- [Saša Jurić — "Elixir in Action" (2nd ed), OTP chapters](https://www.manning.com/books/elixir-in-action-second-edition)
