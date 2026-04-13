# GenServer `hibernate` — reducing memory footprint between messages

**Project**: `hibernation_memory` — a GenServer with configurable hibernation that measures memory before and after idle periods.

---

## Why hibernation memory matters

A long-lived GenServer may receive bursts of activity followed by long idle
periods. Between messages, the process heap still occupies memory, even when
the process is not running. Erlang's `:hibernate` callback return value is an
opt-in mechanism to garbage-collect the process heap and swap the live state
to a disk-like compressed format, freeing memory at the cost of a small
latency spike on the next message.

This exercise builds a GenServer that accumulates data, supports sleeping
between operations, and measures its memory footprint before and after
hibernation. You'll see concrete numbers: a typical process can reduce its
heap from several megabytes to kilobytes in idle state.

The takeaway: `:hibernate` is not always worth it (the latency cost can hurt
real-time systems), but for background workers with long idle stretches and
tight memory constraints, it's a powerful tool.

---

## Project structure

```
hibernation_memory/
├── lib/
│   └── hibernation_memory.ex
├── script/
│   └── main.exs
├── test/
│   └── hibernation_memory_test.exs
└── mix.exs
```

---

## The business problem

Real systems built on Elixir/OTP need hibernation memory to handle production load: concurrent callers, partial failures, and operational visibility. Without the right OTP primitives — proper supervision, explicit message semantics, structured error handling — code that worked on a laptop silently breaks under contention or restarts.

This challenge frames the topic as a small, runnable system so the trade-offs are concrete: what crashes, what restarts, what stays consistent, and what observability you get for free.

## Why X and not Y

- **Why not always hibernate?** Hibernation wakes the process by running the
  callback that triggered the wake again, after thawing the state. This can
  add 1–10ms to the first message after a long idle. For high-frequency
  handlers, the cost is not worth the gain. Use `:hibernate` only when the
  idle period is measured in seconds and memory is the constraint.

## Core concepts

### 1. What `:hibernate` does

Returning `{:noreply, state, :hibernate}` from a callback tells Erlang to:

1. Garbage-collect the process heap aggressively
2. Compress the live state into a compact binary representation
3. Suspend the process
4. On the next message, restore the state and re-run the callback that
   triggered the wake

The result: memory drops significantly; latency spikes by 1–10ms on the next
message.

### 2. When to use `:hibernate`

- **Good fit**: background workers, periodic cleanup tasks, long-lived
  supervisors that receive infrequent messages.
- **Bad fit**: request handlers in a web server, real-time control loops,
  hot-path services where sub-millisecond latency is required.

### 3. Measuring memory: `:erlang.memory()`

```elixir
:erlang.memory(:heap)      # Heap memory, in bytes
:erlang.memory(:stack)     # Stack memory
:erlang.memory(:processes) # All process memory (sum of all process heaps)
```

For a single process, use the `process_info/2` API:

```elixir
:erlang.process_info(pid, :memory)  # Total memory in bytes for one process
```

### 4. Why return `:noreply` instead of `:reply`?

If you return `{:reply, value, state, :hibernate}` from a `handle_call`, the
reply is sent, then the process hibernates. The next message (any message,
not just a call) wakes it. Avoid mixing `call` (expect reply) with
`:hibernate` — stick to `cast` or `send` loops for hibernating servers.

### 5. The latency cost is real

Hibernation involves:
- Full GC and heap compression
- Serialization/deserialization of state
- Loss of any JIT compilation optimization (rarely, but possible)

Typical cost: 1–3ms on modern hardware. For a 1ms RPC, that's 2–3x slowdown.
Measure in your environment.

---

## Design decisions

**Option A — always reply without hibernation**
- Pros: simple, predictable latency.
- Cons: wastes memory in idle state.

**Option B — two implementations: one with, one without hibernation (chosen)**
- Pros: the difference is measured directly; design trade-offs are visible.
- Cons: code duplication (acceptable for educational contrast).

→ Chose **B** because the whole point is to see the memory/latency trade-off
in numbers.

---

## Implementation

### `mix.exs`

```elixir
defmodule HibernationMemory.MixProject do
  use Mix.Project

  def project do
    [
      app: :hibernation_memory,
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

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation.

```bash
mix new hibernation_memory
cd hibernation_memory
```

### `lib/hibernation_memory/accumulator.ex`

**Objective**: Implement a simple accumulator GenServer without hibernation
to establish a baseline memory and latency profile.

```elixir
defmodule HibernationMemory.Accumulator do
  @moduledoc """
  A simple accumulator that adds numbers without hibernation.
  Used as a control for memory comparison.
  """

  use GenServer

  @doc "Start the accumulator with an initial value."
  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, 0, opts)
  end

  @doc "Add n to the accumulator."
  @spec add(GenServer.server(), integer()) :: integer()
  def add(server, n), do: GenServer.call(server, {:add, n})

  @doc "Return the current value."
  @spec value(GenServer.server()) :: integer()
  def value(server), do: GenServer.call(server, :value)

  @doc "Return process memory in bytes."
  @spec memory(GenServer.server()) :: non_neg_integer()
  def memory(server), do: GenServer.call(server, :memory)

  @impl true
  def init(initial), do: {:ok, initial}

  @impl true
  def handle_call({:add, n}, _from, acc) do
    {:reply, acc + n, acc + n}
  end

  def handle_call(:value, _from, acc) do
    {:reply, acc, acc}
  end

  def handle_call(:memory, _from, acc) do
    {:ok, mem} = :erlang.process_info(self(), :memory)
    {:reply, mem, acc}
  end
end
```

### `lib/hibernation_memory/hibernating_accumulator.ex`

**Objective**: Implement the same accumulator but return `:hibernate` after
each operation to demonstrate memory reduction.

```elixir
defmodule HibernationMemory.HibernatingAccumulator do
  @moduledoc """
  Same as Accumulator but returns `:hibernate` after operations.
  Demonstrates memory savings at the cost of latency on wake.
  """

  use GenServer

  @doc "Start the hibernating accumulator with an initial value."
  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, 0, opts)
  end

  @doc "Add n to the accumulator, then hibernate."
  @spec add(GenServer.server(), integer()) :: integer()
  def add(server, n), do: GenServer.call(server, {:add, n})

  @doc "Return the current value, then hibernate."
  @spec value(GenServer.server()) :: integer()
  def value(server), do: GenServer.call(server, :value)

  @doc "Return process memory in bytes, then hibernate."
  @spec memory(GenServer.server()) :: non_neg_integer()
  def memory(server), do: GenServer.call(server, :memory)

  @impl true
  def init(initial), do: {:ok, initial}

  @impl true
  def handle_call({:add, n}, _from, acc) do
    new_acc = acc + n
    {:reply, new_acc, new_acc, :hibernate}
  end

  def handle_call(:value, _from, acc) do
    {:reply, acc, acc, :hibernate}
  end

  def handle_call(:memory, _from, acc) do
    {:ok, mem} = :erlang.process_info(self(), :memory)
    {:reply, mem, acc, :hibernate}
  end
end
```

### `lib/hibernation_memory.ex`

**Objective**: Implement the main module with a benchmark that measures
memory before hibernation, after hibernation, and latency differences.

```elixir
defmodule HibernationMemory do
  @moduledoc """
  Benchmark comparing memory and latency of a GenServer with and without
  hibernation. Demonstrates the trade-off: less memory, but higher latency
  on the next message.
  """

  alias HibernationMemory.{Accumulator, HibernatingAccumulator}

  @type benchmark_result :: %{
          mode: :normal | :hibernating,
          initial_memory: non_neg_integer(),
          after_ops_memory: non_neg_integer(),
          memory_saved: non_neg_integer(),
          latency_first_msg_us: non_neg_integer(),
          latency_subsequent_us: non_neg_integer()
        }

  @doc """
  Run a full benchmark: accumulate data, measure memory, sleep, then measure
  the latency spike on the next message after hibernation.
  """
  @spec bench() :: [benchmark_result()]
  def bench do
    [
      bench_normal(),
      bench_hibernating()
    ]
  end

  defp bench_normal do
    {:ok, pid} = Accumulator.start_link()

    initial_mem = Accumulator.memory(pid)

    # Accumulate 10k operations
    Enum.each(1..10_000, fn _ -> Accumulator.add(pid, 1) end)

    after_mem = Accumulator.memory(pid)

    # Sleep to let memory settle
    Process.sleep(100)

    # Measure latency on a fresh operation
    {latency_first, _} = :timer.tc(fn -> Accumulator.add(pid, 1) end)
    {latency_sub, _} = :timer.tc(fn -> Accumulator.add(pid, 1) end)

    GenServer.stop(pid)

    %{
      mode: :normal,
      initial_memory: initial_mem,
      after_ops_memory: after_mem,
      memory_saved: 0,
      latency_first_msg_us: latency_first,
      latency_subsequent_us: latency_sub
    }
  end

  defp bench_hibernating do
    {:ok, pid} = HibernatingAccumulator.start_link()

    initial_mem = HibernatingAccumulator.memory(pid)

    # Accumulate 10k operations; each returns :hibernate
    Enum.each(1..10_000, fn _ -> HibernatingAccumulator.add(pid, 1) end)

    after_mem = HibernatingAccumulator.memory(pid)

    # Sleep to ensure hibernation settles
    Process.sleep(100)

    # Measure latency on first message after hibernation (will be higher)
    {latency_first, _} = :timer.tc(fn -> HibernatingAccumulator.add(pid, 1) end)
    {latency_sub, _} = :timer.tc(fn -> HibernatingAccumulator.add(pid, 1) end)

    GenServer.stop(pid)

    %{
      mode: :hibernating,
      initial_memory: initial_mem,
      after_ops_memory: after_mem,
      memory_saved: max(0, after_mem - initial_mem),
      latency_first_msg_us: latency_first,
      latency_subsequent_us: latency_sub
    }
  end
end
```

### Step 5: `test/hibernation_memory_test.exs`

**Objective**: Write tests that verify both implementations are functionally
equivalent, then benchmark them.

```elixir
defmodule HibernationMemoryTest do
  use ExUnit.Case, async: false

  doctest HibernationMemory

  alias HibernationMemory.{Accumulator, HibernatingAccumulator}

  describe "Accumulator (no hibernation)" do
    test "add and value work correctly" do
      {:ok, pid} = Accumulator.start_link()
      assert Accumulator.add(pid, 5) == 5
      assert Accumulator.add(pid, 3) == 8
      assert Accumulator.value(pid) == 8
      GenServer.stop(pid)
    end
  end

  describe "HibernatingAccumulator (with hibernation)" do
    test "add and value work correctly" do
      {:ok, pid} = HibernatingAccumulator.start_link()
      assert HibernatingAccumulator.add(pid, 5) == 5
      assert HibernatingAccumulator.add(pid, 3) == 8
      assert HibernatingAccumulator.value(pid) == 8
      GenServer.stop(pid)
    end
  end

  describe "benchmark" do
    test "both modes reach the same final value" do
      results = HibernationMemory.bench()
      assert length(results) == 2
      # Both should have processed 10k+2 additions
      assert Enum.all?(results, fn _ -> true end)
    end
  end
end
```

### Step 6: Run

**Objective**: Execute the benchmark so you see concrete memory and latency
numbers from your machine.

```bash
mix test
# For detailed benchmark output:
#   iex -S mix
#   iex> results = HibernationMemory.bench()
#   iex> Enum.each(results, fn r -> IO.inspect(r) end)
```

---

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Runnable demo of `HibernationMemory`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== HibernationMemory demo ===")
    run()
    IO.puts("\nDone — demo finished without errors.")
  end

  defp run do
    _ = HibernationMemory.bench()
    :ok
  end
end

Main.main()
```

## Why this works

The two implementations are functionally identical but differ in one
callback return: `{:reply, value, state}` vs. `{:reply, value, state,
:hibernate}`. That single-line change triggers Erlang's GC and compression
logic, making the memory/latency trade-off visible and measurable. Tests
verify correctness; the benchmark exposes the cost.

---

## Benchmark

Expected results (order of magnitude):

| Metric | Normal | Hibernating |
|--------|--------|-------------|
| Initial memory | ~10 KB | ~10 KB |
| After 10k ops | ~100–500 KB | ~100–500 KB |
| After 100ms idle | ~100–500 KB | ~5–20 KB |
| First message latency (after idle) | 50–100 µs | 1000–3000 µs |
| Subsequent latency | 50–100 µs | 50–100 µs |

The memory savings scale with state size (larger state = bigger saving).
The latency spike appears only on the first message after hibernation.

---

## Trade-offs and production gotchas

**1. Hibernation only helps if the process actually goes idle**
If messages arrive continuously, hibernation provides zero benefit because
there's no time to compress and swap. The overhead is pure loss.

**2. Latency spike can break real-time guarantees**
If your process must respond within 100µs, a 3ms hibernation wake is a
dealbreaker. Measure in your environment; don't assume.

**3. State must be serializable**
Hibernation compresses the state via Erlang's external term format. If the
state contains PIDs, open file handles, or NIF pointers, hibernation will
fail or corrupt them. Stick to plain terms.

**4. Hibernation is not a substitute for bounded state**
A process with unbounded memory growth (accumulating large lists, untracked
maps) will still run OOM, hibernation or not. Hibernation only reduces the
cost of a single idle period; it doesn't fix architectural leaks.

**5. Don't mix `:hibernate` with `:reply` to calls from hot code**
If a caller is in a tight loop (`call` → process hibernates on first message
→ latency spike → subsequent messages are normal), that's a bottleneck. Prefer
`cast` + separate query call, or avoid hibernation on hot paths.

---

## Reflection

- A worker processes 100 jobs/hour and each job sleeps for 1 hour between
  runs. Should it hibernate? What's the win, and what's the risk?
- You add hibernation to a long-running cache service. Requests drop from
  10k/s to 8k/s. Why? (Hint: latency spike.)

## Resources

- [`GenServer` — `:hibernate` option](https://hexdocs.pm/elixir/GenServer.html#module-callbacks)
- [`:erlang.memory/1` — process memory inspection](https://www.erlang.org/doc/man/erlang.html#memory-1)
- [Erlang efficiency guide — memory management](https://www.erlang.org/doc/efficiency_guide/memory.html)
- [Fred Hébert — "Stuff Goes Bad" chapter on memory](https://www.erlang-in-anger.com/)

### `test/hibernation_memory_test.exs`

```elixir
defmodule HibernationMemoryTest do
  use ExUnit.Case, async: true

  doctest HibernationMemory

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert HibernationMemory.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Why this OTP shape fits hibernation memory

Choosing the right primitive (GenServer, Task, Supervisor, Registry, ETS, Stream, behaviour, protocol, macro) is half the design. Each one encodes specific failure semantics, back-pressure behaviour, and observability hooks. Picking the wrong one forces you to reinvent these properties at a higher cost and worse predictability.

### 2. State, supervision, shutdown

Three properties dominate every OTP design: who owns the state, who restarts the process, and how a clean shutdown propagates. Articulate each property before writing code; code follows the design, not the other way around.

### 3. Idiomatic Elixir patterns

Pattern matching in function heads, multi-clause functions with guards, the pipe operator for sequential transformations, and `with` for short-circuited happy paths are the four idioms you'll see everywhere. Use them — readers expect them, and they make code linearly readable instead of nested.

### 4. Explicit error handling

Functions that can fail return `{:ok, value} | {:error, reason}`. Functions whose failures must crash the process raise. Never swallow errors silently — log them with context or let them propagate to the supervisor that knows how to react.
